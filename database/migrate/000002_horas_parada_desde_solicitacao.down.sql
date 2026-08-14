-- ==========================================================================
-- Reverte para a formula da revisao 4: horas_parada corria desde
-- ordem_servico.aberta_em (definicao original em 000001_schema_inicial.up.sql).
-- ==========================================================================

CREATE OR REPLACE VIEW vw_os_horas AS
SELECT os.id AS ordem_servico_id,
       CASE WHEN os.afeta_producao
            THEN EXTRACT(EPOCH FROM (e.data_fim - os.aberta_em)) / 3600
       END AS horas_parada,
       EXTRACT(EPOCH FROM (e.data_fim - os.iniciada_em)) / 3600
         - COALESCE((SELECT SUM(EXTRACT(EPOCH FROM (p.retomada_em - p.pausada_em))) / 3600
                       FROM os_pausa p
                      WHERE p.ordem_servico_id = os.id
                        AND p.pausada_em >= os.iniciada_em), 0) AS horas_trabalhadas
  FROM ordem_servico os
  JOIN os_encerramento e ON e.ordem_servico_id = os.id;
