package main

import (
	"context"
	"fmt"
	"log"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	"github.com/radaptech/sistema-OSm--Back-end/internal/service"
)

// executarPreventivasVencidas abre uma Solicitação (não uma OS) para cada
// preventiva cuja proxima_data já passou, e avança o ciclo de cada uma. Mesmo
// papel do provisionar-admin e do backup-banco: subcomando de CLI fora da API
// HTTP, chamado pelo Railway Cron.
//
// Não é pg_cron porque o plano Hobby não libera shared_preload_libraries, e
// não é goroutine com ticker dentro da API porque isso amarraria o job ao
// uptime dela. Como CLI, quem dispara é infraestrutura trocável -- cron de VPS,
// CronJob de k8s, GitHub Actions -- sem nada do Railway dentro do código.
//
// Não roda migrations: quem faz isso é o boot da API (main), e o container do
// cron sobe com o mesmo binário contra o mesmo banco já migrado.
//
//	Uso: go run . preventivas-vencidas
func executarPreventivasVencidas(args []string) {
	conf := config.NewVariaveisAmbiente()
	conexao := config.ConnPostgresql{}
	pool, err := conexao.Conn(conf)
	if err != nil {
		log.Fatalf("erro ao conectar no banco: %v", err)
	}
	defer pool.Close()

	criadas, err := service.NewRepoPreventiva(pool).AbrirSolicitacoesDePreventivasVencidas(context.Background())

	// O total sai antes do erro de propósito: falha em uma preventiva não
	// impede as outras, então erro aqui é resultado parcial. Sem imprimir o
	// número primeiro, o log do cron mostraria só o que deu errado e o operador
	// não saberia se as demais rodaram.
	fmt.Printf("preventivas vencidas: %d solicitação(ões) aberta(s)\n", criadas)

	// Ainda assim sai com código != 0: cron que falha metade e devolve sucesso
	// não é notado por ninguém até alguém abrir o log por acaso.
	if err != nil {
		log.Fatalf("preventivas que falharam:\n%v", err)
	}
}
