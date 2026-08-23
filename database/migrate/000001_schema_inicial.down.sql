-- ==========================================================================
-- Solicitacao OS -- Reverte o schema inicial (000001_schema_inicial.up.sql)
-- Ordem exatamente inversa da criacao: views, triggers/funcoes, indices
-- extras, tabelas (das filhas para as raizes) e por fim os tipos ENUM.
-- ==========================================================================

-- --------------------------------------------------------------------------
-- 13. Views calculadas
-- --------------------------------------------------------------------------

DROP VIEW IF EXISTS vw_os_custo_sem_lancamento;
DROP VIEW IF EXISTS vw_os_finalizada;
DROP VIEW IF EXISTS vw_os_horas;

-- --------------------------------------------------------------------------
-- 12. Indices explicitos
-- (os das UNIQUE/PK somem sozinhos com DROP TABLE mais abaixo)
-- --------------------------------------------------------------------------

DROP INDEX IF EXISTS idx_usuario_escopo_setor_setor;
DROP INDEX IF EXISTS idx_usuario_escopo_loja;
DROP INDEX IF EXISTS idx_usuario_escopo_usuario;
DROP INDEX IF EXISTS idx_usuario_area_tecnico;

DROP INDEX IF EXISTS idx_maquina_criticidade;
DROP INDEX IF EXISTS idx_maquina_setor;

DROP INDEX IF EXISTS idx_custo_lancado_por;
DROP INDEX IF EXISTS idx_custo_tenant;
DROP INDEX IF EXISTS idx_encerramento_encerrado_por;
DROP INDEX IF EXISTS idx_encerramento_tenant;
DROP INDEX IF EXISTS idx_pausa_ordem_servico;

DROP INDEX IF EXISTS idx_os_empresa_terceirizada;
DROP INDEX IF EXISTS idx_os_aberta_por;
DROP INDEX IF EXISTS idx_os_urgencia;
DROP INDEX IF EXISTS idx_os_tecnico_status;

DROP INDEX IF EXISTS idx_anexo_solicitacao;
DROP INDEX IF EXISTS idx_solicitacao_rejeitado_por;
DROP INDEX IF EXISTS idx_solicitacao_solicitante;
DROP INDEX IF EXISTS idx_solicitacao_setor;
DROP INDEX IF EXISTS idx_solicitacao_maquina;
DROP INDEX IF EXISTS idx_solicitacao_tenant_status;

-- --------------------------------------------------------------------------
-- 11. Triggers e funcoes
-- --------------------------------------------------------------------------

DROP TRIGGER IF EXISTS trg_usuario_admin_sem_escopo ON usuario;
DROP FUNCTION IF EXISTS fn_check_usuario_vira_admin_sem_escopo();

DROP TRIGGER IF EXISTS trg_usuario_escopo_nao_admin ON usuario_escopo;
DROP FUNCTION IF EXISTS fn_check_usuario_escopo_nao_admin();

DROP TRIGGER IF EXISTS trg_solicitacao_tem_foto ON solicitacao_os;
DROP FUNCTION IF EXISTS fn_check_solicitacao_tem_foto();

DROP TRIGGER IF EXISTS trg_os_tipo_promocao ON ordem_servico;
DROP FUNCTION IF EXISTS fn_os_tipo_promocao();

-- --------------------------------------------------------------------------
-- 6-7. Ordem de Servico e seu ciclo de vida (filhas primeiro)
-- --------------------------------------------------------------------------

DROP TABLE IF EXISTS os_custo;
DROP TABLE IF EXISTS os_encerramento;
DROP TABLE IF EXISTS os_pausa;
DROP TABLE IF EXISTS ordem_servico;

-- --------------------------------------------------------------------------
-- 6. Solicitacao de OS
-- --------------------------------------------------------------------------

DROP TABLE IF EXISTS solicitacao_anexo;
DROP TABLE IF EXISTS solicitacao_impacto;
DROP TABLE IF EXISTS solicitacao_os;

-- --------------------------------------------------------------------------
-- 5. Maquinas e manutencao preventiva
-- --------------------------------------------------------------------------

DROP TABLE IF EXISTS preventiva;
DROP TABLE IF EXISTS maquina;

-- --------------------------------------------------------------------------
-- 4. Usuarios e escopo de acesso
-- --------------------------------------------------------------------------

DROP TABLE IF EXISTS usuario_escopo_setor;
DROP TABLE IF EXISTS usuario_escopo;
DROP TABLE IF EXISTS usuario;

-- --------------------------------------------------------------------------
-- 3. Tabelas de dominio
-- --------------------------------------------------------------------------

DROP TABLE IF EXISTS nivel_urgencia;
DROP TABLE IF EXISTS nivel_criticidade;
DROP TABLE IF EXISTS area_tecnico;

-- --------------------------------------------------------------------------
-- 2. Raiz multi-tenant e estrutura organizacional
-- --------------------------------------------------------------------------

DROP TABLE IF EXISTS empresa_terceirizada;
DROP TABLE IF EXISTS setor;
DROP TABLE IF EXISTS loja;
DROP TABLE IF EXISTS empresa;

-- --------------------------------------------------------------------------
-- 1. Tipos ENUM
-- --------------------------------------------------------------------------

DROP TYPE IF EXISTS tipo_anexo;
DROP TYPE IF EXISTS marcador_impacto;
DROP TYPE IF EXISTS status_os;
DROP TYPE IF EXISTS status_solicitacao;
DROP TYPE IF EXISTS origem_solicitacao;
DROP TYPE IF EXISTS tipo_defeito;
DROP TYPE IF EXISTS tipo_os;
DROP TYPE IF EXISTS tipo_solicitacao;
DROP TYPE IF EXISTS perfil_usuario;

-- --------------------------------------------------------------------------
-- 0. Extensoes
-- --------------------------------------------------------------------------

DROP EXTENSION IF EXISTS citext;
