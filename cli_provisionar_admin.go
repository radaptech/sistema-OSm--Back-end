package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/radaptech/sistema-OSm--Back-end/config"
	
)

// executarProvisionarAdmin cria o primeiro administrador de um tenant (e o
// próprio tenant, se o subdomínio ainda não existir). Roda fora da API
// HTTP -- é chamado direto do main(), antes do servidor subir -- porque
// POST /usuarios exige um administrador já autenticado, e o primeiro não
// existe até este comando rodar.
//
//	Uso: go run . provisionar-admin -subdominio=cooprata -empresa="Cooprata" \
//	       -nome="Davi" -email=admin@cooprata.com -senha=SENHA_FORTE
func executarProvisionarAdmin(args []string) {
	fs := flag.NewFlagSet("provisionar-admin", flag.ExitOnError)
	subdominio := fs.String("subdominio", "", "subdomínio do tenant (ex: cooprata)")
	empresa := fs.String("empresa", "", "nome comercial da empresa — só usado se o subdomínio ainda não existir")
	nome := fs.String("nome", "", "nome do administrador")
	email := fs.String("email", "", "e-mail de login do administrador")
	senha := fs.String("senha", "", "senha do administrador (mínimo 6 caracteres)")
	fs.Parse(args)

	conf := config.NewVariaveisAmbiente()
	conexao := config.ConnPostgresql{}
	pool, err := conexao.Conn(conf)
	if err != nil {
		log.Fatalf("erro ao conectar no banco: %v", err)
	}
	defer pool.Close()

	empresaID, usuarioID, err := ProvisionarAdministrador(context.Background(), pool, DadosProvisionamento{
		Subdominio:  *subdominio,
		NomeEmpresa: *empresa,
		NomeAdmin:   *nome,
		EmailAdmin:  *email,
		SenhaAdmin:  *senha,
	})
	if err != nil {
		log.Fatalf("erro ao provisionar administrador: %v", err)
	}

	fmt.Printf("administrador criado — empresa #%d (%s), usuário #%d (%s)\n", empresaID, *subdominio, usuarioID, *email)
}
