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

	areaID, err := resolverAreaTecnico(ctx, repo, modelUser.Perfil, modelUser.Area, TenantID)
	if err != nil {
		return model.Usuario{}, err
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

	lojasIds, setoresIds, acessoTotal, err := gravarEscopo(ctx, repo, usuario.ID, TenantID, modelUser)
	if err != nil {
		return model.Usuario{}, err
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

// ObterSessao remonta a sessão do dono do token a cada request de
// GET /autenticacao/sessao. É aqui que mora tudo que o JWT não carrega de
// propósito (escopo, área, ativo): editar o escopo de um gestor ou desativar
// um usuário vale na próxima chamada, não só quando o token expirar.
//
// Usuário sumido ou desativado devolve ErrSessaoExpirada -- token válido para
// alguém que não existe mais é sessão morta, não erro de servidor
// (ObterUsuarioPorID não filtra `ativo`, diferente de ObterUsuarioPorEmail).
// A empresa desativada cai no mesmo lugar: sem esse cheque, desativar um
// tenant só barrava login novo (ObterEmpresaPorSubdominio filtra `ativa`) e
// quem já estava dentro seguia trabalhando até o exp.
func (s *UsuarioService) ObterSessao(ctx context.Context, userId, tenantId int64) (model.SessaoUsuario, error) {

	repo := repository.New(s.Pool)

	// Empresa antes do usuário: tenant desativado invalida a sessão de todo
	// mundo dele, não adianta o usuário estar ativo.
	tenantAtivo, err := repo.EmpresaAtiva(ctx, tenantId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.SessaoUsuario{}, helper.ErrSessaoExpirada
		}
		return model.SessaoUsuario{}, helper.TraduzErroPostgres(err)
	}
	if !tenantAtivo {
		return model.SessaoUsuario{}, helper.ErrSessaoExpirada
	}

	user, err := repo.ObterUsuarioPorID(ctx, repository.ObterUsuarioPorIDParams{
		ID:       userId,
		TenantID: tenantId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.SessaoUsuario{}, helper.ErrSessaoExpirada
		}
		return model.SessaoUsuario{}, helper.TraduzErroPostgres(err)
	}

	if !user.Ativo {
		return model.SessaoUsuario{}, helper.ErrSessaoExpirada
	}

	return s.montarSessao(ctx, repo, user, tenantId)
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

// TamanhoPaginaUsuarios é o tamanho de página de GET /usuarios -- o front
// pagina de 10 em 10 em todas as listagens do Administrador
// (front-end/CLAUDE.md item 12).
const TamanhoPaginaUsuarios = 10

// ListarUsuarios devolve uma página de usuários ativos do tenant, cada um com
// o escopo já achatado no formato do front. Sem transação: são só leituras.
//
// `perfil`, `busca` e `lojaId` são opcionais (nil = não filtra). A contagem é
// query própria porque RespostaPaginada exige `total`/`totalPaginas`, e
// LIMIT/OFFSET não sabem quantos ficaram de fora.
func (s *UsuarioService) ListarUsuarios(ctx context.Context, tenantId int64, pagina int32, perfil, busca *string, lojaId *int64) (model.RespostaPaginada[model.Usuario], error) {

	var vazio model.RespostaPaginada[model.Usuario]

	if pagina < 1 {
		pagina = 1
	}

	repo := repository.New(s.Pool)

	total, err := repo.ContarUsuarios(ctx, repository.ContarUsuariosParams{
		TenantID: tenantId,
		Perfil:   (*repository.PerfilUsuario)(perfil),
		Busca:    busca,
		LojaID:   lojaId,
	})
	if err != nil {
		return vazio, helper.TraduzErroPostgres(err)
	}

	usuarios, err := repo.ListarUsuarios(ctx, repository.ListarUsuariosParams{
		TenantID: tenantId,
		Limit:    TamanhoPaginaUsuarios,
		Offset:   (pagina - 1) * TamanhoPaginaUsuarios,
		Perfil:   (*repository.PerfilUsuario)(perfil),
		Busca:    busca,
		LojaID:   lojaId,
	})
	if err != nil {
		return vazio, helper.TraduzErroPostgres(err)
	}

	// Página vazia não é erro (busca sem resultado, ou página além do fim):
	// devolve dados: [] com o total certo, e o front mostra o estado vazio.
	ids := make([]int64, len(usuarios))
	for i, u := range usuarios {
		ids[i] = u.ID
	}

	escopos, err := repo.ObterEscoposSessaoPorUsuarios(ctx, ids)
	if err != nil {
		return vazio, helper.TraduzErroPostgres(err)
	}

	porUsuario := make(map[int64][]repository.ObterEscoposSessaoPorUsuariosRow, len(usuarios))
	for _, e := range escopos {
		porUsuario[e.UsuarioID] = append(porUsuario[e.UsuarioID], e)
	}

	dados := make([]model.Usuario, 0, len(usuarios))
	for _, u := range usuarios {
		dados = append(dados, montarUsuario(u, porUsuario[u.ID]))
	}

	return model.RespostaPaginada[model.Usuario]{
		Dados:        dados,
		Pagina:       pagina,
		TotalPaginas: int32((total + TamanhoPaginaUsuarios - 1) / TamanhoPaginaUsuarios),
		Total:        total,
	}, nil
}

// AtualizarUsuario é PUT /usuarios/:id: dados do usuário e o escopo inteiro,
// numa transação só -- mesmo motivo de CadastrarUsuario, um usuário com o
// perfil novo e o escopo velho enxerga a coisa errada.
//
// O escopo é substituído, nunca mesclado: apaga tudo do usuário e recria com o
// que veio (ver usuario_escopo.sql -- não existe AtualizarEscopo de propósito).
// Os setores saem antes dos escopos, que não há ON DELETE CASCADE entre as
// duas tabelas.
//
// Senha é opcional: omitida, o hash atual fica de pé. Ela tem query própria
// porque AtualizarUsuario não toca em senha_hash.
// ListarTecnicos é GET /tecnicos -- a projeção somente-leitura que alimenta o
// select de "Técnico Responsável" do Gestor. Ver database/queries/usuario.sql
// para o porquê de não ser ListarUsuarios(perfil='tecnico').
//
// lojaId é o filtro que o modal manda (a loja da solicitação); usuarioId/perfil
// são de quem chama e resolvem o escopo -- um gestor não lista os técnicos de
// lojas que ele não enxerga. Mesmo escopoDe() de /maquinas e /preventivas.
func (s *UsuarioService) ListarTecnicos(ctx context.Context, tenantId, usuarioId int64, perfil string, lojaId *int64) ([]model.Tecnico, error) {

	repo := repository.New(s.Pool)

	tecnicos, err := repo.ListarTecnicos(ctx, repository.ListarTecnicosParams{
		TenantID:        tenantId,
		LojaID:          lojaId,
		EscopoUsuarioID: escopoDe(usuarioId, perfil),
	})
	if err != nil {
		return nil, helper.TraduzErroPostgres(err)
	}

	// Não-nil: o front tipa Tecnico[] e null quebraria o .map do select.
	dto := make([]model.Tecnico, 0, len(tecnicos))
	for _, t := range tecnicos {
		dto = append(dto, model.Tecnico{
			Id:       t.ID,
			Nome:     t.Nome,
			Email:    t.Email,
			Telefone: t.Telefone,
			Area:     t.Area,
			LojasIds: t.LojasIds,
		})
	}

	return dto, nil
}

func (s *UsuarioService) AtualizarUsuario(ctx context.Context, id int64, payload model.AtualizarUsuarioPayload, tenantId int64) (model.Usuario, error) {

	// Os dois payloads só diferem na senha; toda a validação de escopo é
	// compartilhada -- ver comoNovoPayload.
	novo := comoNovoPayload(payload)

	if err := validarEscopo(novo); err != nil {
		return model.Usuario{}, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.Usuario{}, fmt.Errorf("erro ao abrir transação: %w", err)
	}
	defer tx.Rollback(ctx)

	repo := repository.New(tx)

	areaID, err := resolverAreaTecnico(ctx, repo, payload.Perfil, payload.Area, tenantId)
	if err != nil {
		return model.Usuario{}, err
	}

	usuario, err := repo.AtualizarUsuario(ctx, repository.AtualizarUsuarioParams{
		ID:            id,
		TenantID:      tenantId,
		Perfil:        repository.PerfilUsuario(payload.Perfil),
		AreaTecnicoID: areaID,
		Nome:          payload.Nome,
		Email:         payload.Email,
		Telefone:      payload.Telefone,
	})
	if err != nil {
		// Id de outro tenant cai aqui igual a id inexistente: o WHERE filtra
		// tenant_id, então não há como editar fora do próprio tenant.
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Usuario{}, helper.ErrNaoEncontrado
		}
		return model.Usuario{}, helper.TraduzErroPostgres(err)
	}

	if payload.Senha != nil {
		senhaHash, err := auth.HashPassword(*payload.Senha)
		if err != nil {
			return model.Usuario{}, fmt.Errorf("erro ao gerar hash da senha: %w", err)
		}
		if err := repo.AtualizarSenhaUsuario(ctx, repository.AtualizarSenhaUsuarioParams{
			ID:        id,
			TenantID:  tenantId,
			SenhaHash: string(senhaHash),
		}); err != nil {
			return model.Usuario{}, helper.TraduzErroPostgres(err)
		}
	}

	if err := repo.DeletarSetoresDosEscoposPorUsuario(ctx, id); err != nil {
		return model.Usuario{}, helper.TraduzErroPostgres(err)
	}
	if err := repo.DeletarEscoposPorUsuario(ctx, id); err != nil {
		return model.Usuario{}, helper.TraduzErroPostgres(err)
	}

	lojasIds, setoresIds, acessoTotal, err := gravarEscopo(ctx, repo, id, tenantId, novo)
	if err != nil {
		return model.Usuario{}, err
	}

	// Os dois gatilhos de administrador-sem-escopo são DEFERRABLE INITIALLY
	// DEFERRED, então virar administrador (ou deixar de ser) só é conferido
	// aqui, contra o estado final -- a ordem das operações acima não importa,
	// mas o erro só aparece no commit.
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

// DesativarUsuario é DELETE /usuarios/:id -- soft delete (ativo = false), ver
// "Soft delete" em docs/modelagem-banco-dados.md: some o cadastro, fica o
// histórico de OS que aponta pro usuário. Não há DELETE de usuário.
//
// Sem transação: é um UPDATE só. O escopo fica onde está de propósito --
// apagá-lo perderia a configuração de acesso de quem for reativado depois, e
// usuário inativo não loga (ObterUsuarioPorEmail filtra `AND ativo`).
//
// atorId é quem está pedindo, e não pode ser o alvo. Não é vaidade: é o que
// garante que sobra pelo menos um administrador ativo no tenant. Só
// administrador chega nesta rota (RBAC), então o último deles se desativando
// tranca o tenant inteiro pra fora -- e a única saída seria a CLI de
// provisionamento.
func (s *UsuarioService) DesativarUsuario(ctx context.Context, id, tenantId, atorId int64) error {

	if id == atorId {
		return fmt.Errorf("%w: um usuário não pode desativar a si mesmo", helper.ErrValidacao)
	}

	repo := repository.New(s.Pool)

	linhas, err := repo.DesativarUsuario(ctx, repository.DesativarUsuarioParams{
		ID:       id,
		TenantID: tenantId,
	})
	if err != nil {
		return helper.TraduzErroPostgres(err)
	}

	// Zero linhas: id inexistente ou de outro tenant (o WHERE filtra os dois).
	// Usuário já inativo casa a linha e conta 1 -- desativar de novo é idempotente.
	if linhas == 0 {
		return helper.ErrNaoEncontrado
	}

	return nil
}

// ObterUsuario é GET /usuarios/:id -- o que a tela de edição
// (/cadastrar-usuario/:id no front) carrega para preencher o formulário.
// Existe porque a edição é deep-linkável: F5 na tela ou link direto não têm
// listagem em cache de onde tirar a linha.
//
// Sem transação: duas leituras. Usa a mesma query em lote de ListarUsuarios
// com um id só, em vez da ObterEscopoSessaoPorUsuario (que devolve outra
// struct), pra montar a resposta com o mesmo montarUsuario -- um formato só
// de Usuario, montado num lugar só.
//
// ObterUsuarioPorID não filtra `ativo`, então um usuário desativado ainda é
// legível aqui. É de propósito: a flag vem no corpo e a listagem já não expõe
// esse id, mas quem tiver o link não recebe um 404 mentiroso.
func (s *UsuarioService) ObterUsuario(ctx context.Context, id, tenantId int64) (model.Usuario, error) {

	repo := repository.New(s.Pool)

	usuario, err := repo.ObterUsuarioPorID(ctx, repository.ObterUsuarioPorIDParams{
		ID:       id,
		TenantID: tenantId,
	})
	if err != nil {
		// tenant_id está no WHERE: id de outro tenant é indistinguível de
		// inexistente, que é exatamente o que o cliente pode saber.
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Usuario{}, helper.ErrNaoEncontrado
		}
		return model.Usuario{}, helper.TraduzErroPostgres(err)
	}

	escopos, err := repo.ObterEscoposSessaoPorUsuarios(ctx, []int64{id})
	if err != nil {
		return model.Usuario{}, helper.TraduzErroPostgres(err)
	}

	return montarUsuario(usuario, escopos), nil
}
