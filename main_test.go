package main

import (
	"slices"
	"testing"
)

// TestDespacharSubcomando tranca o roteamento dos comandos de CLI. Parece
// bobo até lembrar do que ele protege: o Custom Start Command do Railway
// substitui o CMD do dockerfile inteiro, então um comando errado não dá erro
// nenhum -- o container sobe a API no lugar do job e o cron "roda" todo dia
// sem fazer nada. Foi exatamente o que aconteceu com o Cron-BACKUP.
//
// Testa a tabela e o roteamento, nunca a execução: os subcomandos de verdade
// conectam no banco e chamam log.Fatal. Por isso o mapa é substituído por
// fakes -- é ele que o main consulta, então trocar o conteúdo cobre o caminho
// real sem tocar em Postgres.
func TestDespacharSubcomando(t *testing.T) {

	original := subcomandos
	t.Cleanup(func() { subcomandos = original })

	// Os nomes reais precisam continuar existindo: renomear um deles quebra o
	// Cron do Railway, que é configurado por string no painel e não compila
	// junto com este repo.
	for _, nome := range []string{"provisionar-admin", "backup-banco", "preventivas-vencidas"} {
		if _, ok := original[nome]; !ok {
			t.Errorf("subcomando %q sumiu -- o Railway Cron chama por nome", nome)
		}
	}

	var chamado string
	var recebidos []string
	subcomandos = map[string]func([]string){}
	for nome := range original {
		subcomandos[nome] = func(args []string) {
			chamado, recebidos = nome, args
		}
	}

	casos := []struct {
		nome     string
		args     []string
		despacha bool
		alvo     string
		repassa  []string
	}{
		{"sem argumento sobe a API", nil, false, "", nil},
		{"job de preventiva", []string{"preventivas-vencidas"}, true, "preventivas-vencidas", []string{}},
		{"backup", []string{"backup-banco"}, true, "backup-banco", []string{}},
		{
			// O provisionamento é o único com flags, e elas têm que chegar
			// inteiras: o subcomando some da lista, o resto passa direto pro
			// flag.FlagSet dele.
			nome:     "flags do provisionamento chegam sem o nome do subcomando",
			args:     []string{"provisionar-admin", "-subdominio=acme", "-nome=Davi"},
			despacha: true,
			alvo:     "provisionar-admin",
			repassa:  []string{"-subdominio=acme", "-nome=Davi"},
		},
		// Os dois casos de erro humano que o painel do Railway aceita calado.
		{"typo não roda nada", []string{"preventiva-vencidas"}, false, "", nil},
		{"caminho no lugar do subcomando", []string{"./main"}, false, "", nil},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			chamado, recebidos = "", nil

			if got := despacharSubcomando(c.args); got != c.despacha {
				t.Fatalf("despacharSubcomando(%v) = %v, esperado %v", c.args, got, c.despacha)
			}
			if chamado != c.alvo {
				t.Fatalf("executou %q, esperado %q", chamado, c.alvo)
			}
			if c.despacha && !slices.Equal(recebidos, c.repassa) {
				t.Errorf("repassou %v, esperado %v", recebidos, c.repassa)
			}
		})
	}
}
