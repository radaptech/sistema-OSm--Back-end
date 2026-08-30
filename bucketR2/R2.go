package bucketr2

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	r2_config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TamanhoMaximoFoto é o teto do corpo inteiro da requisição com foto (e o teto
// de memória de QUALQUER requisição multipart, mesmo as que aceitam corpo
// maior -- ver TamanhoMaximoComVideo). Exportado porque quem corta o body é o
// handler (http.MaxBytesReader, que precisa do ResponseWriter) -- aqui só
// existe o número, num lugar só.
const TamanhoMaximoFoto = 10 << 20 // 10MB

// TamanhoMaximoComVideo é o teto do corpo em POST /solicitacoes/maquinario --
// a única rota que aceita vídeo (front-end/src/componentes/UploadVideo.tsx
// já corta em 8s/40MB no cliente; isto é o teto do servidor, não a regra de
// negócio). Continua usando TamanhoMaximoFoto como teto de MEMÓRIA em
// ParseMultipartForm -- são números diferentes de propósito: o corpo pode
// chegar a 40MB, mas o que fica na RAM do processo continua 10MB, o resto
// escorre pro arquivo temporário do disco.
const TamanhoMaximoComVideo = 40 << 20 // 40MB

var s3Client *s3.Client
var presignClient *s3.PresignClient

func InitR2_cloudflare(ctx context.Context) {

	idconta := os.Getenv("R2_IDCLOUDFLARE")
	secretKey := os.Getenv("R2_SECRETKEY")
	keyid := os.Getenv("R2_KEYID")

	config, err := r2_config.LoadDefaultConfig(ctx,
		r2_config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(keyid, secretKey, "")),
		r2_config.WithRegion("auto"))
	if err != nil {
		log.Fatalf("erro ao carregar configuraçoes da aws: %v", err)
	}

	s3Client = s3.NewFromConfig(config, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", idconta))
	})
	presignClient = s3.NewPresignClient(s3Client)

}

// UploadFoto sobe o arquivo recebido no multipart e devolve a KEY do objeto --
// nunca uma URL. A leitura é sempre assinada na hora (URLLeitura, TTL curto):
// bucket público num sistema multi-tenant deixaria qualquer um com o link ver
// a foto de outro tenant, e URL persistida vira link morto sem avisar.
//
// Recebe o *multipart.FileHeader e não um gin.Context de propósito: quem sabe
// se a foto é obrigatória, qual o status do erro e o que fazer quando o resto
// da transação falha é o handler do domínio -- este pacote só fala com o R2.
// Abrir e fechar o arquivo fica aqui para o chamador não esquecer o Close.
//
// A key é prefixada por tenant (tenant/{id}/...) para isolar os arquivos entre
// empresas, e o ContentType vem do header do arquivo: sem ele o R2 serve como
// application/octet-stream e o browser baixa em vez de exibir.
func UploadFoto(ctx context.Context, tenantID int64, bucket string, header *multipart.FileHeader) (string, error) {

	// s3Client nasce nil e só é montado por InitR2_cloudflare: sem esta guarda
	// um boot sem as variáveis do R2 derruba o processo com nil pointer no
	// primeiro upload, em vez de devolver erro.
	if s3Client == nil {
		return "", fmt.Errorf("R2 não inicializado")
	}

	if bucket == "" {
		return "", fmt.Errorf("bucket do R2 não configurado")
	}

	arquivo, err := header.Open()
	if err != nil {
		return "", fmt.Errorf("erro ao abrir a foto enviada: %w", err)
	}
	defer arquivo.Close()

	ext := filepath.Ext(header.Filename)
	objkey := fmt.Sprintf("tenant/%d/%d%s", tenantID, time.Now().UnixNano(), ext)

	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(objkey),
		Body:        arquivo,
		ContentType: aws.String(header.Header.Get("Content-Type")),
	})
	if err != nil {
		log.Printf("erro ao salvar foto no r2 bucket=%s key=%s: %v", bucket, objkey, err)
		return "", fmt.Errorf("erro ao salvar no R2")
	}

	return objkey, nil
}

// UploadArquivo sobe um conteúdo já em mãos (io.Reader), sem passar por
// multipart.FileHeader -- usado por quem não recebeu o arquivo numa requisição
// HTTP, como o dump do backup (cli_backup_banco.go). A key vem pronta do
// chamador: diferente de UploadFoto, aqui não existe tenant nem timestamp
// implícito, porque quem precisa dessa forma varia por caso de uso.
func UploadArquivo(ctx context.Context, bucket, key string, corpo io.Reader, contentType string) error {

	if s3Client == nil {
		return fmt.Errorf("R2 não inicializado")
	}

	if bucket == "" {
		return fmt.Errorf("bucket do R2 não configurado")
	}

	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        corpo,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		log.Printf("erro ao salvar arquivo no r2 bucket=%s key=%s: %v", bucket, key, err)
		return fmt.Errorf("erro ao salvar no R2")
	}

	return nil
}

func URLLeitura(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {

	// Mesma guarda do UploadFoto: presignClient nasce nil e só é montado por
	// InitR2_cloudflare. Sem ela, um boot sem as variáveis do R2 vira panic de
	// nil pointer na primeira máquina com foto que alguém listar -- e aí não é
	// só a foto que some, é a resposta inteira.
	if presignClient == nil {
		return "", fmt.Errorf("R2 não inicializado")
	}

	resul, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		log.Printf("erro ao gerar url da foto: %v", err)
		return "", fmt.Errorf("erro ao gerar url da foto")
	}

	return resul.URL, nil
}
