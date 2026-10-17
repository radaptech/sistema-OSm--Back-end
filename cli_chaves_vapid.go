package main

import (
	"fmt"
	"log"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// executarChavesVapid gera um par de chaves VAPID (Web Push) e imprime as
// linhas prontas pro .env. Não toca em banco nem em arquivo -- quem cola no
// .env (e no Railway) é você.
//
// Gere UMA vez por ambiente e não troque à toa: todo aparelho se inscreve com
// a chave pública, então trocar o par invalida as inscrições existentes (os
// envios passam a falhar até cada usuário reativar as notificações).
//
//	Uso: go run . chaves-vapid   (ou make chaves-vapid)
func executarChavesVapid(_ []string) {

	privada, publica, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		log.Fatalf("erro ao gerar chaves VAPID: %v", err)
	}

	fmt.Println("# Web Push (VAPID) -- a privada é segredo, como a senha do banco.")
	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", publica)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", privada)
	fmt.Println("VAPID_SUBJECT=contato@radaptech.com.br")
}
