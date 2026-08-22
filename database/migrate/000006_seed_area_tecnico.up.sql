-- ==========================================================================
-- area_tecnico: tenant novo nasce com as areas padrao.
--
-- O problema: nada populava a tabela. Nem migration, nem a CLI de
-- provisionamento, nem CRUD -- e o front exige `area` obrigatoria para o
-- perfil Tecnico (areasTecnico em front-end/src/tipos/tecnico.ts). Resultado:
-- em TODO tenant, cadastrar tecnico falhava com "registro nao encontrado:
-- area tecnica ... nao cadastrada neste tenant" (resolverAreaTecnico ->
-- ObterAreaTecnicoPorNome), e sem tecnico o Gestor nao consegue abrir OS
-- nenhuma. Era o bloqueio mais a montante do sistema.
--
-- ⚠️ Por que NAO virou ENUM como nivel_criticidade (migration 000004): a
-- secao 2.4 de docs/modelagem-banco-dados.md da uma razao explicita para esta
-- continuar tabela -- "um supermercado pode querer 'Automacao' onde outro
-- quer 'Ar-condicionado'". Criticidade nao tinha essa razao registrada. Aqui
-- o conserto preserva a customizacao por tenant: as cinco areas sao um ponto
-- de partida, nao uma lista fechada.
--
-- O trigger e o que resolve o caso que uma migration sozinha nao alcanca:
-- migration roda uma vez, e tenant nasce depois dela (make provisionar-admin).
-- Sem ele, todo tenant criado a partir de amanha voltaria a nascer travado.
-- ==========================================================================

-- Dedupe antes da constraint. Nada popula esta tabela hoje, entao duplicata so
-- existe se alguem inseriu na mao -- mas migration que falha nao e feature
-- quebrada, e a API inteira fora do ar (main.go da log.Fatal no erro do
-- RunMigrationPostgress). "UNIQUE em coluna que ja tem duplicata" e o primeiro
-- item da lista de o-que-passa-no-vazio-e-quebra-com-dado do CLAUDE.md.
--
-- Mantem a linha de menor id -- a mais antiga, e a que tem mais chance de ja
-- estar referenciada -- e repontua os usuarios das outras antes de apagar,
-- senao a FK recusaria o DELETE.
WITH sobrevivente AS (
    SELECT tenant_id, nome, min(id) AS id_mantido
    FROM area_tecnico
    GROUP BY tenant_id, nome
    HAVING count(*) > 1
)
UPDATE usuario u
SET area_tecnico_id = s.id_mantido
FROM area_tecnico a
JOIN sobrevivente s ON s.tenant_id = a.tenant_id AND s.nome = a.nome
WHERE u.area_tecnico_id = a.id
  AND a.id <> s.id_mantido;

DELETE FROM area_tecnico a
USING area_tecnico b
WHERE a.tenant_id = b.tenant_id
  AND a.nome = b.nome
  AND a.id > b.id;

-- Unicidade por nome, que faltava: sem ela o seed rodando duas vezes duplica
-- a area, e ObterAreaTecnicoPorNome e :one -- passaria a devolver "a
-- primeira" das duas, calado. Tambem e o que torna o seed idempotente
-- (ON CONFLICT DO NOTHING abaixo).
ALTER TABLE area_tecnico
    ADD CONSTRAINT uq_area_tecnico_nome UNIQUE (tenant_id, nome);

-- Uma funcao so, usada pelo backfill e pelo trigger: a lista de areas padrao
-- vive num lugar unico. Espelha areasTecnico do front, na mesma ordem.
CREATE FUNCTION fn_seed_area_tecnico(p_tenant_id bigint) RETURNS void AS $$
    INSERT INTO area_tecnico (tenant_id, nome)
    VALUES
        (p_tenant_id, 'Refrigeração'),
        (p_tenant_id, 'Elétrica'),
        (p_tenant_id, 'Mecânica'),
        (p_tenant_id, 'Hidráulica'),
        (p_tenant_id, 'Máquinas em Geral')
    ON CONFLICT ON CONSTRAINT uq_area_tecnico_nome DO NOTHING;
$$ LANGUAGE sql;

CREATE FUNCTION fn_empresa_seed_area_tecnico() RETURNS trigger AS $$
BEGIN
    PERFORM fn_seed_area_tecnico(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- AFTER INSERT: a linha da empresa precisa existir antes, area_tecnico.tenant_id
-- referencia empresa (id).
CREATE TRIGGER trg_empresa_seed_area_tecnico
    AFTER INSERT ON empresa
    FOR EACH ROW
    EXECUTE FUNCTION fn_empresa_seed_area_tecnico();

-- Backfill dos tenants que ja existem. ON CONFLICT dentro da funcao: quem ja
-- tinha 'Elétrica' cadastrada na mao mantem a linha (e o id) que ja esta
-- referenciada em usuario.area_tecnico_id.
SELECT fn_seed_area_tecnico(id) FROM empresa;
