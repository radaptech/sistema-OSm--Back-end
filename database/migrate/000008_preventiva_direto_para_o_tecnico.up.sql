-- ==========================================================================
-- Preventiva vencida vai direto para o Tecnico, sem passar pelo Gestor.
--
-- Ate aqui a preventiva vencida abria uma solicitacao 'Pendente' e parava na
-- fila do Gestor, que escolhia tecnico e urgencia para a OS nascer. Mas
-- preventiva e trabalho ja aprovado no momento em que a maquina foi
-- cadastrada: o Administrador definiu o procedimento, o intervalo e a data.
-- Pedir uma segunda aprovacao a cada ciclo nao decide nada e so atrasa.
--
-- A solicitacao CONTINUA existindo, nascendo ja 'Convertida' junto com a OS,
-- na mesma transacao do job. Nao e detalhe: horas_parada e medida desde
-- solicitacao_os.criado_em (vw_os_horas), uq_os_solicitacao exige uma
-- solicitacao por OS, e o card do Tecnico busca a solicitacao de origem para
-- mostrar o problema. Sem ela, tres coisas quebram de uma vez.
--
-- Tres alteracoes, uma por bloco abaixo.
-- ==========================================================================

-- --------------------------------------------------------------------------
-- 1. O tecnico da preventiva
-- --------------------------------------------------------------------------
-- Quem o Gestor escolhia a cada ciclo passa a ser escolhido uma vez, no
-- cadastro da preventiva. Preventiva e trabalho recorrente e previsivel: o
-- mesmo compressor volta para o mesmo tecnico de refrigeracao todo mes. A
-- alternativa (sortear por area e loja no job) precisaria de uma regra de
-- desempate que nao existe e escolheria calado um tecnico afastado.
--
-- ⚠️ NULLABLE de proposito, apesar de o servico exigir o campo na escrita.
-- NOT NULL sem default falha em tabela que ja tem linha, e derrubar a
-- migration derruba o boot da API (main.go faz log.Fatal) -- e o boot e o
-- unico lugar onde ela roda. Preventiva antiga fica sem tecnico ate alguem
-- editar a maquina; o job trata isso como falha visivel, nao como linha
-- ignorada em silencio (ver abrirSolicitacaoDaPreventiva).
--
-- FK composta com tenant_id, como todas as outras: e o que impede a
-- preventiva do tenant A de apontar para o tecnico do tenant B. Com
-- tecnico_id NULL o MATCH SIMPLE do Postgres nao checa o par, que e
-- exatamente o comportamento desejado -- mesmo caso de
-- fk_solicitacao_solicitante.
ALTER TABLE preventiva ADD COLUMN tecnico_id bigint;

ALTER TABLE preventiva ADD CONSTRAINT fk_preventiva_tecnico
    FOREIGN KEY (tenant_id, tecnico_id) REFERENCES usuario (tenant_id, id);

CREATE INDEX idx_preventiva_tecnico ON preventiva (tecnico_id);

-- --------------------------------------------------------------------------
-- 2. OS aberta por ninguem
-- --------------------------------------------------------------------------
-- aberta_por_id era NOT NULL porque toda OS nascia de um clique do Gestor.
-- A OS de preventiva nasce de uma data no calendario: nao ha ator, e inventar
-- um (o Administrador que cadastrou, o tecnico que vai executar) gravaria uma
-- autoria falsa numa coluna que existe justamente para responder "quem abriu".
--
-- NULL aqui significa "o sistema", o mesmo que solicitante_id NULL ja
-- significa em solicitacao_os de origem 'preventiva'. Quem le a coluna
-- distingue os dois casos por ordem_servico -> solicitacao_os.origem.
ALTER TABLE ordem_servico ALTER COLUMN aberta_por_id DROP NOT NULL;

-- --------------------------------------------------------------------------
-- 3. A protecao contra ciclo duplicado muda de lugar
-- --------------------------------------------------------------------------
-- uq_preventiva_pendente era um indice unico parcial em solicitacao_os
-- (preventiva_id) WHERE status = 'Pendente'. Ele impedia que duas replicas do
-- cron rodando juntas criassem duas solicitacoes para o mesmo ciclo -- a
-- corrida entre o SELECT das vencidas e o INSERT.
--
-- Com a solicitacao nascendo 'Convertida', o filtro do indice nunca casa e
-- ele para de proteger qualquer coisa. Ampliar o filtro tambem nao resolve:
-- uma preventiva PODE ter varias solicitacoes ao longo do tempo, uma por
-- ciclo, entao nenhum unique sobre (preventiva_id) sozinho serve.
--
-- A protecao passa a ser o lock da propria preventiva: o job faz
-- ObterPreventivaVencidaParaAbertura (SELECT ... FOR UPDATE) dentro da
-- transacao e reconfere a proxima_data antes de inserir. A segunda replica
-- espera o commit da primeira, le a data ja avancada e desiste sem escrever
-- nada. E mais forte que o indice: cobre as duas escritas, nao so o INSERT.
DROP INDEX uq_preventiva_pendente;
