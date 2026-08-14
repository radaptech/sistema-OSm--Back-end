-- ==========================================================================
-- Solicitacao OS -- revisao 4.1 (secao 4 de docs/modelagem-banco-dados.md)
--
-- horas_parada passa a correr desde solicitacao_os.criado_em, e nao mais
-- desde ordem_servico.aberta_em: a maquina parou quando o Solicitante
-- relatou o problema, nao quando o Gestor achou tempo de abrir a OS. Medindo
-- da abertura, toda a espera na fila do Gestor sumia do indicador -- e quanto
-- mais devagar ele aprovasse, melhor o numero ficava.
--
-- Nenhuma coluna nasce ou muda: a grandeza nunca foi armazenada, entao o
-- valor novo sai no proximo SELECT, retroativo por construcao. O join e
-- seguro por definicao -- ordem_servico.solicitacao_id e NOT NULL e UNIQUE
-- (uq_os_solicitacao, secao 5.3), entao o INNER JOIN nao descarta nem
-- duplica linha nenhuma.
--
-- CREATE OR REPLACE serve porque a lista de colunas (nome, tipo e ordem)
-- continua identica -- o Postgres so recusa REPLACE quando ela muda.
-- ==========================================================================

CREATE OR REPLACE VIEW vw_os_horas AS
SELECT os.id AS ordem_servico_id,
       -- Parada corre sem interrupcao e nao desconta as pausas do tecnico: a
       -- maquina segue parada enquanto ele espera uma peca (secao 4).
       CASE WHEN os.afeta_producao
            THEN EXTRACT(EPOCH FROM (e.data_fim - s.criado_em)) / 3600
       END AS horas_parada,
       EXTRACT(EPOCH FROM (e.data_fim - os.iniciada_em)) / 3600
         - COALESCE((SELECT SUM(EXTRACT(EPOCH FROM (p.retomada_em - p.pausada_em))) / 3600
                       FROM os_pausa p
                      WHERE p.ordem_servico_id = os.id
                        AND p.pausada_em >= os.iniciada_em), 0) AS horas_trabalhadas
  FROM ordem_servico os
  JOIN os_encerramento e ON e.ordem_servico_id = os.id
  JOIN solicitacao_os s ON s.id = os.solicitacao_id;
