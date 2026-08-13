package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
		return nil, fmt.Errorf("setor inexistente neste tenant: %w", helper.ErrNaoEncontrado)
	}

	porLoja := make(map[int64][]int64, len(lojasIds))
	for _, s := range setores {
		if !slices.Contains(lojasIds, s.LojaID) {
			return nil, fmt.Errorf("setor %d não pertence a nenhuma das lojas selecionadas", s.ID)
		}
		porLoja[s.LojaID] = append(porLoja[s.LojaID], s.ID)
	}

	// Loja sem setor nenhum vira escopo que não enxerga nada -- é erro de
	// preenchimento, não acesso total (esse é o acessoTotalSetores).
	for _, idLoja := range lojasIds {
		if len(porLoja[idLoja]) == 0 {
			return nil, fmt.Errorf("loja %d ficou sem nenhum setor selecionado", idLoja)
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
			return errors.New("solicitante precisa de exatamente 1 loja e 1 setor")
		}
	case "tecnico":
		if len(p.LojasIds) == 0 {
			return errors.New("técnico precisa de ao menos 1 loja")
		}
		if p.Area == nil || *p.Area == "" {
			return errors.New("técnico precisa de uma área")
		}
	case "gestor":
		if len(p.LojasIds) == 0 {
			return errors.New("gestor precisa de ao menos 1 loja")
		}
		if !p.AcessoTotalSetores && len(p.SetoresIds) == 0 {
			return errors.New("gestor precisa de ao menos 1 setor ou acesso total aos setores")
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
