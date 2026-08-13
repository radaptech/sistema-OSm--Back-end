package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/radaptech/sistema-OSm--Back-end/auth"
	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

type UsuarioService struct {
	Pool *pgxpool.Pool
}

func NewRepoUsuario(pool *pgxpool.Pool) *UsuarioService {

	return &UsuarioService{
		Pool: pool,
	}
}

// CadastrarUsuario cria o usuário e o escopo de acesso dele numa única
// transação: usuário sem escopo (fora administrador) não enxerga nada, então
// ou os dois entram ou nenhum entra.
func (s *UsuarioService) CadastrarUsuario(ctx context.Context, modelUser model.NovoUsuarioPayload, TenantID int64) (model.Usuario, error) {

	if err := validarEscopo(modelUser); err != nil {
		return model.Usuario{}, err
	}

	senhaHash, err := auth.HashPassword(modelUser.Senha)
	if err != nil {
		return model.Usuario{}, fmt.Errorf("erro ao gerar hash da senha: %w", err)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.Usuario{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	// area_tecnico_id é NOT NULL exatamente quando perfil = 'tecnico'
	// (ck_usuario_area_tecnico) -- o front manda o nome, o banco quer o id.
	var areaID *int16
	if modelUser.Perfil == "tecnico" {
		id, err := repo.ObterAreaTecnicoPorNome(ctx, repository.ObterAreaTecnicoPorNomeParams{
			TenantID: TenantID,
			Nome:     *modelUser.Area,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return model.Usuario{}, fmt.Errorf("área técnica %q não cadastrada neste tenant: %w", *modelUser.Area, helper.ErrNaoEncontrado)
			}
			return model.Usuario{}, helper.TraduzErroPostgres(err)
		}
		areaID = &id
	}

	usuario, err := repo.CriarUsuario(ctx, repository.CriarUsuarioParams{
		TenantID:      TenantID,
		Perfil:        repository.PerfilUsuario(modelUser.Perfil),
		AreaTecnicoID: areaID,
		Nome:          modelUser.Nome,
		Email:         modelUser.Email,
		SenhaHash:     string(senhaHash),
		Telefone:      modelUser.Telefone,
	})
	if err != nil {
		return model.Usuario{}, helper.TraduzErroPostgres(err)
	}

	lojasIds, setoresIds, acessoTotal := escopoDoPerfil(modelUser)

	// Cada setor entra só no escopo da própria loja -- ver setoresPorLoja.
	var porLoja map[int64][]int64
	if len(setoresIds) > 0 {
		porLoja, err = setoresPorLoja(ctx, repo, TenantID, lojasIds, setoresIds)
		if err != nil {
			return model.Usuario{}, err
		}
	}

	for _, idLoja := range lojasIds {

		escopo, err := repo.CriarEscopo(ctx, repository.CriarEscopoParams{
			UsuarioID:          usuario.ID,
			LojaID:             idLoja,
			AcessoTotalSetores: acessoTotal,
		})
		if err != nil {
			return model.Usuario{}, helper.TraduzErroPostgres(err)
		}

		// acesso_total_setores = true não tem linha de setor: a ausência é o
		// acesso total à loja (docs/modelagem-banco-dados.md 3.8).
		for _, idSetor := range porLoja[idLoja] {
			err := repo.CriarEscopoSetor(ctx, repository.CriarEscopoSetorParams{
				EscopoID: escopo.ID,
				SetorID:  idSetor,
			})
			if err != nil {
				return model.Usuario{}, helper.TraduzErroPostgres(err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Usuario{}, helper.TraduzErroPostgres(err)
	}

	return model.Usuario{
		Id:                 usuario.ID,
		Nome:               usuario.Nome,
		Telefone:           usuario.Telefone,
		Email:              usuario.Email,
		Perfil:             string(usuario.Perfil),
		LojasIds:           lojasIds,
		SetoresIds:         setoresIds,
		AcessoTotalSetores: acessoTotal,
		Ativo:              usuario.Ativo,
	}, nil
}

// Login autentica no tenant vindo do header X-tenant-ID (o único momento em
// que ele manda -- depois disso o tenant autoritativo é o do token) e devolve
// o JWT já assinado + a sessão que o front consome.
//
// Sem transação de propósito: são leituras + um UPDATE de ultimo_acesso que
// não precisam ser atômicos entre si.
func (s *UsuarioService) Login(ctx context.Context, loginModel model.Login, tenantId int64) (string, model.SessaoUsuario, error) {

	repo := repository.New(s.Pool)

	// ObterUsuarioPorEmail já filtra `AND ativo`: usuário desativado cai no
	// mesmo ErrNoRows de e-mail inexistente.
	user, err := repo.ObterUsuarioPorEmail(ctx, repository.ObterUsuarioPorEmailParams{
		TenantID: tenantId,
		Email:    loginModel.Email,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", model.SessaoUsuario{}, helper.ErrCredenciaisInvalidas
		}
		return "", model.SessaoUsuario{}, helper.TraduzErroPostgres(err)
	}

	senhaOk, err := auth.HashCompare([]byte(user.SenhaHash), loginModel.Senha)
	if err != nil || !senhaOk {
		return "", model.SessaoUsuario{}, helper.ErrCredenciaisInvalidas
	}

	// O perfil vem do formulário de login, então é palpite do cliente: se não
	// bate com o do banco, é credencial errada -- nunca promove ninguém.
	if string(user.Perfil) != loginModel.Perfil {
		return "", model.SessaoUsuario{}, helper.ErrCredenciaisInvalidas
	}

	sessao, err := s.montarSessao(ctx, repo, user, tenantId)
	if err != nil {
		return "", model.SessaoUsuario{}, err
	}

	token, err := auth.GerarJwt(user.ID, tenantId, string(user.Perfil))
	if err != nil {
		return "", model.SessaoUsuario{}, fmt.Errorf("erro ao gerar token jwt: %w", err)
	}

	err = repo.RegistrarUltimoAcesso(ctx, repository.RegistrarUltimoAcessoParams{
		ID:       user.ID,
		TenantID: tenantId,
	})
	if err != nil {
		return "", model.SessaoUsuario{}, helper.TraduzErroPostgres(err)
	}

	return token, sessao, nil
}
