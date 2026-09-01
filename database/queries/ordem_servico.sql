-- ==========================================================================
-- Ordem de serviço -- leitura.
--
-- A escrita mora em solicitacao_os.sql (CriarOrdemServicoDeSolicitacao, o
-- POST /solicitacoes/:id/abrir-os): a OS nasce da aprovação do Gestor, não
-- de um POST /ordens-servico. Este arquivo é só o outro lado -- a listagem
-- que os três painéis consomem, e mais tarde o ciclo de vida
-- (iniciar/pausar/encerrar/custo, fase 2).
-- ==========================================================================

-- name: ListarOrdensServico :many
-- GET /ordens-servico -- um endpoint para os três painéis, o que muda é o
-- filtro (front-end/src/servicos/servicoOrdensServico.ts):
--   Gestor  (PainelGestor)                 -> sem filtro, recorta pelo escopo
--   Técnico (PainelTecnico)                -> ?tecnicoId=
--   Admin   (CustosPendentes/OSFinalizadas)-> ?status=Concluída / ?finalizada=true
-- Array simples, sem paginação: o front tipa `OrdemServico[]` e pagina no
-- cliente, mesmo padrão de ListarSolicitacoes/ListarMaquinas. `pagina` existe
-- em ParametrosListagemOrdensServico mas nunca chega a uma query -- ignorar é
-- o comportamento certo, não um esquecimento.
--
-- os_pausa fica de fora: é 1:N e sairia duplicando a OS por pausa. Vem de
-- ObterPausasDasOrdensServico (abaixo), em lote, mesmo padrão de
-- ObterAnexosDasSolicitacoes.
--
-- JOINs INNER onde a FK é NOT NULL (solicitação, setor, loja, técnico) e LEFT
-- onde a coluna é nullable (máquina e solicitante -- reparo não tem máquina,
-- preventiva não tem pessoa; empresa terceirizada -- só existe se o Técnico
-- acionou uma) ou onde a linha inteira pode não existir ainda
-- (os_encerramento até o Técnico encerrar, os_custo até o custo ser lançado,
-- vw_os_horas que é INNER em os_encerramento por dentro).
--
-- ⚠️ horas_* e custo_* são projetadas CRUAS, sem `::float8`, e isso não é
-- descuido: o override que as transforma em pgtype.Float8 mora no sqlc.yaml e
-- casa por NOME DE COLUNA. Com o cast o sqlc perde o vínculo com a coluna, o
-- override não casa mais E a expressão passa a ser tipada como NOT NULL --
-- sai `float64` e o Scan quebra no primeiro NULL (que é o caso comum: OS
-- aberta não tem horas nem custo). Ver a nota longa no sqlc.yaml.
--
-- `finalizada` é derivado, não coluna (docs/modelagem, seção 3.4): a OS está
-- finalizada quando o Técnico encerrou E o custo foi lançado. Mesma regra de
-- vw_os_finalizada, que não serve aqui porque ela é `JOIN os_custo` e só
-- devolve as finalizadas -- a listagem precisa de todas.
-- ⚠️ A expressão aparece DUAS vezes (projeção e filtro) porque o Postgres não
-- deixa referenciar alias do SELECT no WHERE -- mesmo motivo de
-- ListarUsuarios/ContarUsuarios repetirem o WHERE delas. Divergir as duas dá
-- uma listagem que se contradiz.
-- ⚠️ O `::boolean` no fim não é enfeite: expressão booleana composta sem cast
-- vira `*bool` no sqlc, mesma armadilha do `vencida` em preventiva.sql.
--
-- custoTotal NÃO é calculado aqui, de propósito: é a soma dos dois valores
-- que já viajam na linha, e uma expressão a mais no SELECT seria mais uma
-- chance de cair na armadilha do numeric/COALESCE. O model soma.
--
-- ⚠️ area_tecnico é LEFT, diferente de ListarTecnicos, que a junta INNER. Lá
-- o WHERE já garante perfil = 'tecnico', e ck_usuario_area_tecnico exige a
-- coluna NOT NULL exatamente nesse perfil. Aqui não: fk_os_tecnico aponta pra
-- `usuario`, sem checar o perfil, e AtualizarUsuario zera area_tecnico_id ao
-- tirar alguém do perfil técnico. Com INNER, promover a gestor um técnico que
-- já tem OS aberta apagaria essas OS da listagem inteira, calado. O front
-- tipa tecnicoArea como opcional -- NULL cabe, sumir não.
--
-- ⚠️ `status` entra como text[] e só vira status_os dentro do ANY. Como
-- status_os[] direto, o pgx não acha plano de encode ("unknown type (OID
-- ...): cannot find encode plan") -- ele conhece os tipos built-in, não um
-- ARRAY de ENUM nosso, e registrar o tipo no pool custaria um AfterConnect
-- em config/conn.go por enum. O cast volta para status_os antes da
-- comparação de propósito: com `os.status::text = ANY(...)` o índice
-- idx_os_tecnico_status pararia de valer para esta coluna.
--
-- Escopo no WHERE via EXISTS, mesmo bloco de ListarSolicitacoes: NULL é o
-- administrador (não filtra), os demais perfis só enxergam OS cujo setor eles
-- alcançam. O setor é o da solicitação de origem -- ordem_servico não tem
-- setor_id próprio, e nem deveria: a OS é da solicitação, não de um lugar.
SELECT
    os.*,
    s.descricao,
    s.criado_em AS data_solicitacao,
    s.maquina_id,
    s.item_descricao,
    s.setor_id,
    m.nome AS maquina_nome,
    m.numero_patrimonio AS maquina_codigo,
    sc.nome AS setor_nome,
    sc.loja_id,
    l.nome AS loja_nome,
    sol.nome AS solicitante_nome,
    tec.nome AS tecnico_nome,
    a.nome AS tecnico_area,
    et.nome AS empresa_terceirizada_nome,
    (os.status = 'Concluída' AND c.id IS NOT NULL)::boolean AS finalizada,
    e.tipo_defeito,
    e.data_fim,
    e.defeito_constatado,
    e.causa_raiz,
    e.solucao,
    enc.nome AS encerrado_por_nome,
    h.horas_trabalhadas,
    h.horas_parada,
    c.custo_hora_tecnico,
    c.custo_manutencao,
    c.numero_nota_fiscal,
    c.serie_nota_fiscal,
    c.descricao_servico_terceiro,
    c.lancado_em,
    lanc.nome AS lancado_por_nome
FROM ordem_servico os
JOIN solicitacao_os s  ON s.tenant_id = os.tenant_id AND s.id = os.solicitacao_id
JOIN setor sc          ON sc.tenant_id = s.tenant_id AND sc.id = s.setor_id
JOIN loja l            ON l.tenant_id = sc.tenant_id AND l.id = sc.loja_id
JOIN usuario tec       ON tec.tenant_id = os.tenant_id AND tec.id = os.tecnico_id
LEFT JOIN area_tecnico a           ON a.tenant_id = tec.tenant_id AND a.id = tec.area_tecnico_id
LEFT JOIN maquina m                ON m.tenant_id = s.tenant_id AND m.id = s.maquina_id
LEFT JOIN usuario sol              ON sol.tenant_id = s.tenant_id AND sol.id = s.solicitante_id
LEFT JOIN empresa_terceirizada et  ON et.tenant_id = os.tenant_id AND et.id = os.empresa_terceirizada_id
LEFT JOIN os_encerramento e        ON e.tenant_id = os.tenant_id AND e.ordem_servico_id = os.id
LEFT JOIN usuario enc              ON enc.tenant_id = e.tenant_id AND enc.id = e.encerrado_por_id
LEFT JOIN os_custo c               ON c.tenant_id = os.tenant_id AND c.ordem_servico_id = os.id
LEFT JOIN usuario lanc             ON lanc.tenant_id = c.tenant_id AND lanc.id = c.lancado_por_id
LEFT JOIN vw_os_horas h            ON h.ordem_servico_id = os.id
WHERE os.tenant_id = sqlc.arg(tenant_id)
  AND (
    sqlc.narg(finalizada)::boolean IS NULL
    OR (os.status = 'Concluída' AND c.id IS NOT NULL) = sqlc.narg(finalizada)
  )
  AND (sqlc.narg(status)::text[] IS NULL OR os.status = ANY(sqlc.narg(status)::text[]::status_os[]))
  AND (sqlc.narg(tipo)::tipo_os IS NULL OR os.tipo = sqlc.narg(tipo))
  AND (sqlc.narg(loja_id)::bigint IS NULL OR sc.loja_id = sqlc.narg(loja_id))
  AND (sqlc.narg(tecnico_id)::bigint IS NULL OR os.tecnico_id = sqlc.narg(tecnico_id))
  AND (
    sqlc.narg(busca)::text IS NULL
    OR s.descricao ILIKE '%' || sqlc.narg(busca) || '%'
    OR m.nome ILIKE '%' || sqlc.narg(busca) || '%'
    OR s.item_descricao ILIKE '%' || sqlc.narg(busca) || '%'
  )
  AND (
    sqlc.narg(escopo_usuario_id)::bigint IS NULL
    OR EXISTS (
      SELECT 1
      FROM usuario_escopo ue
      LEFT JOIN usuario_escopo_setor ues ON ues.escopo_id = ue.id
      WHERE ue.usuario_id = sqlc.narg(escopo_usuario_id)
        AND ue.loja_id = sc.loja_id
        AND (ue.acesso_total_setores OR ues.setor_id = s.setor_id)
    )
  )
ORDER BY os.aberta_em DESC;

-- name: ObterPausasDasOrdensServico :many
-- Pausas de uma página inteira de OS numa ida só, mesmo padrão de
-- ObterAnexosDasSolicitacoes: 1:N não cabe na listagem sem duplicar a OS por
-- pausa, e uma query por OS seria N+1.
--
-- os_pausa não tem tenant_id (nem precisa: ela pende de ordem_servico, que
-- tem) -- o recorte de tenant e de escopo já aconteceu em
-- ListarOrdensServico, e é de lá que saem os ids. Nunca chame esta query com
-- ids que não vieram de uma listagem já filtrada.
--
-- ORDER BY pausada_em: o front lê `pausas` como histórico em ordem e tira
-- `pausaAtual` da que tem retomada_em nulo (uq_pausa_aberta garante no máximo
-- uma por OS).
SELECT id, ordem_servico_id, status_anterior, motivo, pausada_em, retomada_em
FROM os_pausa
WHERE ordem_servico_id = ANY(sqlc.arg(ordens_servico_ids)::bigint[])
ORDER BY ordem_servico_id, pausada_em;

-- name: ListarHistoricoOsDaMaquina :many
-- GET /indicadores/maquinas/:id -- a matéria-prima do Painel de Indicadores do
-- Gestor (front DashboardGestor). Uma linha por OS encerrada da máquina; as
-- seis grandezas do painel (Horas Parada, MTTR, MTBF, Custo Total, rosca por
-- tipo de defeito e barras de custo mensal) são somadas em
-- MontarIndicadoresMaquina, não aqui.
--
-- Agregar no SELECT custaria seis expressões e três GROUP BY diferentes numa
-- query só, e MTBF (média do intervalo entre aberturas) ainda pediria LAG --
-- em Go são três laços sobre uma lista que já cabe na memória, testáveis sem
-- Postgres. Mesmo espírito do custoTotal de ListarOrdensServico, que também é
-- somado no model.
--
-- O JOIN com os_encerramento É o filtro de "concluída": a linha só existe
-- quando o Técnico encerrou (uq_encerramento_os, data_fim NOT NULL) e não há
-- reabertura no modelo. `os.status = 'Concluída'` seria o espelho
-- denormalizado da mesma coisa -- e, se um dia divergirem, é a linha de
-- encerramento que manda.
--
-- Sem escopo no WHERE aqui, e não é esquecimento: quem valida o acesso é o
-- ObterMaquinaPorID que o service chama ANTES, com escopo_usuario_id. Máquina
-- fora do escopo nem chega nesta query -- vira 404 lá em cima.
--
-- ⚠️ horas_* e custo_* CRUAS, sem ::float8 -- ver a nota longa em
-- ListarOrdensServico e no sqlc.yaml: o cast quebra o override que as
-- transforma em pgtype.Float8, e o Scan estoura no primeiro NULL (custo ainda
-- não lançado, ou máquina que não parou).
--
-- O mês sai pronto do banco, em America/Sao_Paulo, mesmo tratamento de
-- `vencida` em preventiva.sql: uma OS encerrada às 22h de 31/08 é agosto pro
-- Gestor e setembro pro UTC. Formato YYYY-MM porque ordena como texto -- o
-- 'MM/YYYY' do contrato é montado na hora de responder.
SELECT
    os.aberta_em,
    e.tipo_defeito,
    to_char(e.data_fim AT TIME ZONE 'America/Sao_Paulo', 'YYYY-MM') AS mes_encerramento,
    h.horas_parada,
    h.horas_trabalhadas,
    c.custo_hora_tecnico,
    c.custo_manutencao
FROM ordem_servico os
JOIN solicitacao_os s   ON s.tenant_id = os.tenant_id AND s.id = os.solicitacao_id
JOIN os_encerramento e  ON e.tenant_id = os.tenant_id AND e.ordem_servico_id = os.id
JOIN vw_os_horas h      ON h.ordem_servico_id = os.id
LEFT JOIN os_custo c    ON c.tenant_id = os.tenant_id AND c.ordem_servico_id = os.id
WHERE os.tenant_id = sqlc.arg(tenant_id)
  AND s.maquina_id = sqlc.arg(maquina_id)::bigint
-- Ascendente porque MTBF é a média do intervalo entre aberturas consecutivas:
-- ordenado aqui, o Go só percorre. Trocar para DESC quebra o indicador calado.
ORDER BY os.aberta_em;
