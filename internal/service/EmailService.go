package service

import (
	"context"
	"fmt"
	"time"

	"github.com/resend/resend-go/v2"
)

// urlFrontendPadrao é o front local atrás do traefik do compose (http://<tenant>.localhost:8090).
const urlFrontendPadrao = "http://%s.localhost:8090"

type EmailService struct {
	client *resend.Client
	// urlFrontendFormato tem um %s onde entra o subdomínio do tenant: o link precisa abrir no front da empresa certa.
	urlFrontendFormato string
}

// NewEmailService recebe as variáveis prontas: quem lê os.Getenv é o wiring (router.go), igual NewRepoNotificacao.
func NewEmailService(apiKey, urlFrontendFormato string) *EmailService {

	if urlFrontendFormato == "" {
		urlFrontendFormato = urlFrontendPadrao
	}

	return &EmailService{
		client:             resend.NewClient(apiKey),
		urlFrontendFormato: urlFrontendFormato,
	}
}

func (e *EmailService) RecuperaEmail(ctx context.Context, emailDestino, token, slugEmpresa string) error {

	link := fmt.Sprintf("%s/redefinir-senha?token=%s", fmt.Sprintf(e.urlFrontendFormato, slugEmpresa), token)

	// Cores e textos espelham a TelaLogin do front (paleta `marca` do tailwind.config.ts).
	corpo := fmt.Sprintf(`
		<div style="background-color: #2f6e42; padding: 40px 16px; font-family: 'IBM Plex Sans', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;">
			<div style="max-width: 448px; margin: 0 auto; background-color: #ffffff; border-radius: 24px; overflow: hidden; box-shadow: 0 25px 50px -12px rgba(15, 41, 22, 0.4);">
				<div style="padding: 40px 32px 32px;">
					<div style="text-align: center; margin-bottom: 28px;">
						<div style="display: inline-block; width: 48px; height: 48px; line-height: 48px; border-radius: 16px; background-color: #e4f6e9; color: #2f6e42; font-size: 22px;">&#128295;</div>
						<h1 style="margin: 12px 0 0; color: #0f172a; font-size: 26px; font-weight: 700; letter-spacing: -0.5px;">Solicitação OS</h1>
						<p style="margin: 4px 0 0; color: #94a3b8; font-family: 'IBM Plex Mono', Menlo, Consolas, monospace; font-size: 12px; font-weight: 700; letter-spacing: 2px; text-transform: uppercase;">Recuperação de senha</p>
					</div>

					<p style="color: #334155; font-size: 15px; line-height: 24px; margin: 0 0 16px;">Olá,</p>
					<p style="color: #334155; font-size: 15px; line-height: 24px; margin: 0 0 28px;">Recebemos uma solicitação para redefinir a senha da sua conta. Para criar uma nova senha, clique no botão abaixo. O link vale por <strong>30 minutos</strong> e só pode ser usado uma vez.</p>

					<div style="text-align: center; margin-bottom: 28px;">
						<a href="%s" style="background-color: #2f6e42; color: #ffffff; padding: 14px 28px; text-decoration: none; border-radius: 12px; display: inline-block; font-weight: 700; font-size: 14px; box-shadow: 0 1px 2px rgba(47, 110, 66, 0.2);">Criar nova senha</a>
					</div>

					<p style="color: #64748b; font-size: 13px; line-height: 20px; margin: 0 0 8px;">Ou copie e cole o link abaixo no seu navegador:</p>
					<p style="margin: 0; word-break: break-all;"><a href="%s" style="color: #2f6e42; font-size: 13px; text-decoration: underline;">%s</a></p>

					<p style="color: #94a3b8; font-size: 12px; line-height: 18px; margin: 28px 0 0;">Se você não solicitou esta alteração, ignore este e-mail. Sua senha continua a mesma.</p>
				</div>

				<div style="border-top: 1px solid #f1f5f9; padding: 16px 32px; text-align: center;">
					<span style="color: #a5d6b7; font-family: 'IBM Plex Mono', Menlo, Consolas, monospace; font-size: 10px; font-weight: 600; letter-spacing: 2px; text-transform: uppercase;">Solicitação OS © %d</span>
				</div>
			</div>
		</div>
	`, link, link, link, time.Now().Year())

	_, err := e.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    "Solicitação OS <contato@radaptech.com.br>",
		To:      []string{emailDestino},
		Subject: "Recuperação de senha - Solicitação OS",
		Html:    corpo,
		// Versão em texto puro: e-mail só com HTML pontua como spam nos filtros (MIME_HTML_ONLY).
		Text: fmt.Sprintf("Olá,\n\nRecebemos uma solicitação para redefinir a senha da sua conta no Solicitação OS.\n\n"+
			"Para criar uma nova senha, acesse o link abaixo (vale por 30 minutos e só pode ser usado uma vez):\n%s\n\n"+
			"Se você não solicitou esta alteração, ignore este e-mail. Sua senha continua a mesma.\n", link),
	})

	return err
}
