package config

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
	// tzdata embutido no binário (~450KB): a imagem de produção é alpine pelada,
	// sem /usr/share/zoneinfo, e sem isto o LoadLocation abaixo falharia lá e
	// passaria no dev.
	_ "time/tzdata"
)

// Brasilia é o fuso de TODA data que entra e sai da API, e ele não vem do
// ambiente de propósito.
//
// ⚠️ O pgx decodifica timestamptz com time.Unix(), que devolve no fuso LOCAL do
// processo Go -- e container não tem TZ por padrão (nem o do compose, nem o do
// Railway), então local é UTC. Sem converter aqui, uma OS aberta 21:56 de 31/08
// aparecia no painel como 00:56 de 01/09: instante certo, fuso errado, três
// horas no futuro e um dia à frente.
//
// Consertar por TZ=America/Sao_Paulo no compose funcionaria no dev e deixaria a
// produção quebrada até alguém lembrar da variável lá também. O fuso é regra do
// contrato ("DataBr"), não configuração de host -- mesmo raciocínio do
// AT TIME ZONE que preventiva.sql e ListarHistoricoOsDaMaquina já usam para não
// depender do fuso de quem roda a query.
var Brasilia = carregarBrasilia()

// Panica no boot em vez de cair num fixo -03: com o tzdata embutido isto só
// falha se a stdlib estiver quebrada, e o horário de verão brasileiro (existiu
// até 2019) faz de qualquer offset fixo uma resposta errada para data antiga.
// Falhar alto na subida é melhor que servir UTC calado, que é o bug de origem.
func carregarBrasilia() *time.Location {
	local, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic("não foi possível carregar o fuso America/Sao_Paulo: " + err.Error())
	}
	return local
}

type DataBr time.Time

func (d *DataBr) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "null" || s == "" {
		return nil
	}
	// Layout brasileiro com data e hora: DD/MM/YYYY HH:MM:SS.
	// ParseInLocation e não Parse: o texto não carrega offset, e Parse assumiria
	// UTC -- "31/08/2026 21:56:00" viraria 18:56 de Brasília na ida.
	t, err := time.ParseInLocation("02/01/2006 15:04:05", s, Brasilia)
	if err != nil {
		// Sem hora: o contrato aceita as duas formas, e campo de coluna `date`
		// chega assim -- preventiva.proxima_data vem de um <input type="date">
		// que o front converte para dd/mm/yyyy antes de enviar. Sem este
		// fallback, cadastrar preventiva falhava na borda, antes do service.
		t, err = time.ParseInLocation("02/01/2006", s, Brasilia)
		if err != nil {
			return fmt.Errorf("formato de data inválido. Use DD/MM/YYYY HH:MM:SS ou DD/MM/YYYY")
		}
	}
	*d = DataBr(t)
	return nil
}

// Devolve o JSON formatado como DD/MM/YYYY HH:MM:SS
func (d *DataBr) MarshalJSON() ([]byte, error) {
	t := time.Time(*d)
	if t.IsZero() {
		return []byte("null"), nil
	}
	// .In(Brasilia) é o ponto do conserto: o t que vem do pgx está no fuso local
	// do processo (UTC no container), e Format usa o fuso que o valor carrega.
	return fmt.Appendf(nil, "\"%s\"", t.In(Brasilia).Format("02/01/2006 15:04:05")), nil
}

func NewDataBrPtr(t time.Time) *DataBr {
	d := DataBr(t)
	return &d
}

func (d *DataBr) Time() time.Time {
	return time.Time(*d)
}

func (d *DataBr) IsZero() bool {
	return time.Time(*d).IsZero()
}

func (d DataBr) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return time.Time(d), nil
}
