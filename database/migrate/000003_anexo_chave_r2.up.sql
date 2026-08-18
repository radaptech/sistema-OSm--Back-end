-- ==========================================================================
-- Storage real dos anexos via Cloudflare R2 (docs/modelagem-banco-dados.md,
-- secao 3.10 e "Pontos em aberto" 1).
--
-- Renomeia so o rotulo das colunas -- o conteudo ja era, por convencao, a
-- chave do objeto no storage, nunca uma URL persistida (URL assinada expira;
-- guardar uma URL fixa acumularia link morto sem indicio de que quebrou).
-- O nome antigo (url/foto_url) so ficava enganoso perto da implementacao de
-- verdade. Sem coluna `bucket`: o bucket de cada tipo de anexo e fixo no
-- codigo que registra a rota (bucketR2.UploadFoto(url, bucket)), nao varia
-- por linha.
-- ==========================================================================

ALTER TABLE maquina RENAME COLUMN foto_url TO foto_chave;
ALTER TABLE solicitacao_anexo RENAME COLUMN url TO chave;
