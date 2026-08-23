package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/radaptech/sistema-OSm--Back-end/database/repository"
	"github.com/radaptech/sistema-OSm--Back-end/internal/helper"
	"github.com/radaptech/sistema-OSm--Back-end/internal/model"
)

// escopoDoPerfil normaliza o payload plano do front no que cada perfil pode
// ter de escopo (3.8). Os campos que não valem pro perfil são descartados
// aqui, não rejeitados -- é o que o front documenta ("ignorados pelo servidor
// quando perfil é 'tecnico' ou 'administrador'").
func escopoDoPerfil(p model.NovoUsuarioPayload) (lojasIds, setoresIds []int64, acessoTotal bool) {

	lojasIds, setoresIds = []int64{}, []int64{}

	switch p.Perfil {
	case "administrador":
		// nenhum escopo: a ausência É o acesso total ao tenant
		// (trg_usuario_escopo_nao_admin recusa qualquer linha).
		return lojasIds, setoresIds, false
	case "tecnico":
		return p.LojasIds, setoresIds, true
	case "solicitante":
		return p.LojasIds, p.SetoresIds, false
	default: // gestor
		if p.AcessoTotalSetores {
			return p.LojasIds, setoresIds, true
		}
		return p.LojasIds, p.SetoresIds, false
	}
}

// setoresPorLoja distribui a lista plana de setoresIds entre as lojas a que
// eles de fato pertencem (`setor.loja_id`).
//
// O payload do front é plano -- um `setoresIds` só para N `lojasIds` --, mas
// setor pertence a uma loja: jogar a lista inteira em toda loja gravaria
// escopo dizendo que o usuário acessa "Padaria da Loja A" dentro da Loja B.
// Como o id do setor já identifica a loja, a lista plana **expressa** o escopo
// por loja; é só distribuir. (O que ela ainda não expressa é acesso total numa
// loja e parcial noutra -- `acessoTotalSetores` é um flag só, global.)
//
// Valida de quebra o que o banco não pega: `usuario_escopo_setor` só tem FK
// para `setor (id)`, sem checar se esse setor é da loja do escopo.
func setoresPorLoja(ctx context.Context, repo *repository.Queries, tenantID int64, lojasIds, setoresIds []int64) (map[int64][]int64, error) {

	setores, err := repo.ObterSetoresPorIDs(ctx, repository.ObterSetoresPorIDsParams{
		Ids:      setoresIds,
		TenantID: tenantID,
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}
	if len(setores) != len(setoresIds) {
		// Sentinela na frente: com "%w" no fim, o texto genérico do sentinela
		// fica pendurado no rabo da mensagem que o front mostra no toast.
		return nil, fmt.Errorf("%w: setor inexistente neste tenant", helper.ErrNaoEncontrado)
	}

	porLoja := make(map[int64][]int64, len(lojasIds))
	for _, s := range setores {
		if !slices.Contains(lojasIds, s.LojaID) {
			return nil, fmt.Errorf("%w: setor %d não pertence a nenhuma das lojas selecionadas", helper.ErrValidacao, s.ID)
		}
		porLoja[s.LojaID] = append(porLoja[s.LojaID], s.ID)
	}

	// Loja sem setor nenhum vira escopo que não enxerga nada -- é erro de
	// preenchimento, não acesso total (esse é o acessoTotalSetores).
	for _, idLoja := range lojasIds {
		if len(porLoja[idLoja]) == 0 {
			return nil, fmt.Errorf("%w: loja %d ficou sem nenhum setor selecionado", helper.ErrValidacao, idLoja)
		}
	}

	return porLoja, nil
}

// validarEscopo cobre a cardinalidade por perfil, que as tags de binding do
// Gin não alcançam (elas só validam formato) -- ver 3.8.
func validarEscopo(p model.NovoUsuarioPayload) error {

	switch p.Perfil {
	case "administrador":
		return nil
	case "solicitante":
		if len(p.LojasIds) != 1 || len(p.SetoresIds) != 1 {
			return fmt.Errorf("%w: solicitante precisa de exatamente 1 loja e 1 setor", helper.ErrValidacao)
		}
	case "tecnico":
		if len(p.LojasIds) == 0 {
			return fmt.Errorf("%w: técnico precisa de ao menos 1 loja", helper.ErrValidacao)
		}
		if p.Area == nil || *p.Area == "" {
			return fmt.Errorf("%w: técnico precisa de uma área", helper.ErrValidacao)
		}
	case "gestor":
		if len(p.LojasIds) == 0 {
			return fmt.Errorf("%w: gestor precisa de ao menos 1 loja", helper.ErrValidacao)
		}
		if !p.AcessoTotalSetores && len(p.SetoresIds) == 0 {
			return fmt.Errorf("%w: gestor precisa de ao menos 1 setor ou acesso total aos setores", helper.ErrValidacao)
		}
	}

	return nil
}

// montarSessao resolve fresco no banco tudo que o JWT não carrega de
// propósito (escopo, área, ativo) -- é o mesmo corpo de GET
// /autenticacao/sessao, por isso está separado do Login.
func (s *UsuarioService) montarSessao(ctx context.Context, repo *repository.Queries, user repository.Usuario, tenantId int64) (model.SessaoUsuario, error) {

	sessao := model.SessaoUsuario{
		Id:     user.ID,
		Nome:   user.Nome,
		Email:  user.Email,
		Perfil: string(user.Perfil),
	}

	switch user.Perfil {

	case repository.PerfilUsuarioAdministrador:
		// Sem escopo: a ausência É o acesso total ao tenant.
		return sessao, nil

	case repository.PerfilUsuarioTecnico:
		// tecnicoId é o próprio usuario.id (fk_os_tecnico aponta pra usuario).
		sessao.TecnicoId = &user.ID
		return sessao, nil
	}

	escopos, err := repo.ObterEscopoSessaoPorUsuario(ctx, user.ID)
	if err != nil {
		return model.SessaoUsuario{}, helper.TraduzErroPostgres(err)
	}
	if len(escopos) == 0 {
		return model.SessaoUsuario{}, fmt.Errorf("usuário %d sem escopo de acesso cadastrado", user.ID)
	}

	if user.Perfil == repository.PerfilUsuarioGestor {
		sessao.EscoposGestor = make([]model.EscopoAcessoGestor, 0, len(escopos))
		for _, e := range escopos {
			sessao.EscoposGestor = append(sessao.EscoposGestor, model.EscopoAcessoGestor{
				LojaId:     e.LojaID,
				SetoresIds: model.SetoresIds{AcessoTotal: e.AcessoTotalSetores, Ids: e.SetoresIds},
			})
		}
		return sessao, nil
	}

	// Solicitante: exatamente 1 loja e 1 setor (garantido no cadastro).
	escopo := escopos[0]
	if len(escopo.SetoresIds) == 0 {
		return model.SessaoUsuario{}, fmt.Errorf("solicitante %d sem setor no escopo", user.ID)
	}

	setor, err := repo.ObterSetorPorID(ctx, repository.ObterSetorPorIDParams{
		ID:       escopo.SetoresIds[0],
		TenantID: tenantId,
	})
	if err != nil {
		return model.SessaoUsuario{}, helper.TraduzErroPostgres(err)
	}

	sessao.LojaId = &escopo.LojaID
	sessao.SetorId = &setor.ID
	sessao.SetorNome = &setor.Nome

	return sessao, nil
}

// montarUsuario achata o escopo do banco (uma linha por loja, com a lista de
// setores) no formato plano que o front espera em Usuario -- lojasIds,
// setoresIds e um acessoTotalSetores só. É o caminho inverso de
// escopoDoPerfil.
//
// ⚠️ A perda é a mesma lacuna já conhecida do payload de escrita: como
// acessoTotalSetores é um flag global, "total na loja A e parcial na B" não
// tem como voltar inteiro. Só é true quando TODOS os escopos são totais --
// caso contrário o front marcaria o alternador e apagaria os setores da loja
// parcial ao salvar de novo. Some quando o contrato virar
// escopos: [{lojaId, setoresIds}] (ver front-end/CLAUDE.md item 7).
func montarUsuario(u repository.Usuario, escopos []repository.ObterEscoposSessaoPorUsuariosRow) model.Usuario {

	// Slices não-nil de propósito: o front tipa lojasIds/setoresIds como
	// number[], e nil viraria `null` no JSON.
	lojasIds, setoresIds := []int64{}, []int64{}
	acessoTotal := len(escopos) > 0

	for _, e := range escopos {
		lojasIds = append(lojasIds, e.LojaID)
		setoresIds = append(setoresIds, e.SetoresIds...)
		acessoTotal = acessoTotal && e.AcessoTotalSetores
	}

	return model.Usuario{
		Id:                 u.ID,
		Nome:               u.Nome,
		Telefone:           u.Telefone,
		Email:              u.Email,
		Perfil:             string(u.Perfil),
		LojasIds:           lojasIds,
		SetoresIds:         setoresIds,
		AcessoTotalSetores: acessoTotal,
		Ativo:              u.Ativo,
	}
}

// resolverAreaTecnico traduz o nome da área (o que o front manda) no
// area_tecnico_id que a coluna guarda. Devolve nil fora do perfil técnico:
// ck_usuario_area_tecnico exige area_tecnico_id NOT NULL exatamente quando
// perfil = 'tecnico', então trocar o perfil de técnico para outro tem que
// zerar a área junto -- é por isso que quem chama nunca preserva o valor
// antigo.
func resolverAreaTecnico(ctx context.Context, repo *repository.Queries, perfil string, area *string, tenantID int64) (*int16, error) {

	if perfil != "tecnico" {
		return nil, nil
	}
	if area == nil {
		return nil, fmt.Errorf("%w: técnico exige área de atuação", helper.ErrValidacao)
	}

	id, err := repo.ObterAreaTecnicoPorNome(ctx, repository.ObterAreaTecnicoPorNomeParams{
		TenantID: tenantID,
		Nome:     *area,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: área técnica %q não cadastrada neste tenant", helper.ErrNaoEncontrado, *area)
		}
		return nil, helper.TraduzErroPostgres(err)
	}

	return &id, nil
}

// gravarEscopo cria as linhas de usuario_escopo (+ usuario_escopo_setor) do
// usuário e devolve o escopo já normalizado, no formato plano que a resposta
// usa. Compartilhado por CadastrarUsuario e AtualizarUsuario -- no update quem
// chama apaga o conjunto antigo antes, porque escopo se substitui inteiro e
// não se mescla (ver usuario_escopo.sql).
func gravarEscopo(ctx context.Context, repo *repository.Queries, usuarioID, tenantID int64, p model.NovoUsuarioPayload) (lojasIds, setoresIds []int64, acessoTotal bool, err error) {

	lojasIds, setoresIds, acessoTotal = escopoDoPerfil(p)

	// Cada setor entra só no escopo da própria loja -- ver setoresPorLoja.
	var porLoja map[int64][]int64
	if len(setoresIds) > 0 {
		porLoja, err = setoresPorLoja(ctx, repo, tenantID, lojasIds, setoresIds)
		if err != nil {
			return nil, nil, false, err
		}
	}

	for _, idLoja := range lojasIds {

		escopo, err := repo.CriarEscopo(ctx, repository.CriarEscopoParams{
			UsuarioID:          usuarioID,
			LojaID:             idLoja,
			AcessoTotalSetores: acessoTotal,
		})
		if err != nil {
			return nil, nil, false, helper.TraduzErroPostgres(err)
		}

		// acesso_total_setores = true não tem linha de setor: a ausência é o
		// acesso total à loja (docs/modelagem-banco-dados.md 3.8).
		for _, idSetor := range porLoja[idLoja] {
			if err := repo.CriarEscopoSetor(ctx, repository.CriarEscopoSetorParams{
				EscopoID: escopo.ID,
				SetorID:  idSetor,
			}); err != nil {
				return nil, nil, false, helper.TraduzErroPostgres(err)
			}
		}
	}

	return lojasIds, setoresIds, acessoTotal, nil
}

// comoNovoPayload existe só para AtualizarUsuario reaproveitar validarEscopo,
// escopoDoPerfil e gravarEscopo, todos escritos sobre NovoUsuarioPayload. Os
// dois payloads só diferem na senha (opcional no update), que não tem nada a
// ver com escopo -- duplicar as três funções por causa disso deixaria a regra
// de cardinalidade em dois lugares para divergir.
func comoNovoPayload(p model.AtualizarUsuarioPayload) model.NovoUsuarioPayload {
	return model.NovoUsuarioPayload{
		Nome:               p.Nome,
		Telefone:           p.Telefone,
		Email:              p.Email,
		Perfil:             p.Perfil,
		LojasIds:           p.LojasIds,
		SetoresIds:         p.SetoresIds,
		AcessoTotalSetores: p.AcessoTotalSetores,
		Area:               p.Area,
	}
}
