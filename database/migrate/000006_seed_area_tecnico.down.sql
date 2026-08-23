-- Ordem inversa exata do up: linhas semeadas -> trigger -> funcoes -> constraint.

-- Apaga so as cinco areas padrao, e so as que ninguem esta usando: uma area
-- referenciada por usuario.area_tecnico_id nao pode sumir (a FK recusaria e o
-- down inteiro falharia), e area que o cliente cadastrou nao e desta migration.
DELETE FROM area_tecnico a
WHERE a.nome IN ('Refrigeração','Elétrica','Mecânica','Hidráulica','Máquinas em Geral')
  AND NOT EXISTS (SELECT 1 FROM usuario u WHERE u.area_tecnico_id = a.id);

DROP TRIGGER trg_empresa_seed_area_tecnico ON empresa;

DROP FUNCTION fn_empresa_seed_area_tecnico();

DROP FUNCTION fn_seed_area_tecnico(bigint);

ALTER TABLE area_tecnico DROP CONSTRAINT uq_area_tecnico_nome;
