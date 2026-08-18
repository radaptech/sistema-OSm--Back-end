package bucketr2

import (
	"context"

	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	r2_config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/radaptech/sistema-OSm--Back-end/middleware"
)

const tamanhoMaximoFoto = 10 << 20 // 10MB

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

func UploadFoto(fotoUrl, bucket string) gin.HandlerFunc {

	return func(ctx *gin.Context) {

		if ctx.Request.Method != http.MethodPost {

			log.Printf("metado nao permitido para fazer o upload da imagem: %v", ctx.Request.Method)
			ctx.JSON(http.StatusMethodNotAllowed, gin.H{
				"erro": "metado nao permitido",
			})
			return
		}

		tenantId, ok := middleware.GetTenantIDToken(ctx)
		if !ok {
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"erro": "erro interno de tenant",
			})
			return
		}

		//limitando o tamanho da imagem: MaxBytesReader corta a leitura do body,
		//ParseMultipartForm sozinho só limita o que fica em memória
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, tamanhoMaximoFoto)
		if err := ctx.Request.ParseMultipartForm(tamanhoMaximoFoto); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"erro":     "erro ao ler o formulario enviado",
				"detalhes": err.Error(),
			})
			return
		}

		file, header, err := ctx.Request.FormFile("foto")
		if err != nil {

			ctx.JSON(http.StatusBadRequest, gin.H{
				"erro":     "erro ao ler a foto enviada",
				"detalhes": err.Error(),
			})
			return
		}
		defer file.Close()

		//nome dos arquivos, prefixado por tenant pra isolar o bucket entre empresas
		//(sem repetir o nome do bucket na key: a key já vive dentro dele)
		ext := filepath.Ext(header.Filename)
		objkey := fmt.Sprintf("tenant/%d/%d%s", tenantId, time.Now().UnixNano(), ext)

		_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(objkey),
			Body:        file,
			ContentType: aws.String(header.Header.Get("Content-Type")),
		})
		if err != nil {

			log.Printf("erro ao salvar foto no r2: %v", err)
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"erro": "erro ao salvar no R2",
			})
			return
		}

		ctx.JSON(http.StatusOK, gin.H{
			"status":  "sucesso",
			"arquivo": objkey,
		})

	}
}

func URLLeitura(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {

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
