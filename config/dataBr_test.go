package config

import (
	"encoding/json"
	"testing"
	"time"
)

// O bug que este teste tranca: o pgx decodifica timestamptz no fuso LOCAL do
// processo, e container sem TZ é UTC. Uma OS aberta às 21:56 de 31/08 saía como
// "01/09/2026 00:56" no painel -- três horas no futuro e um dia à frente.
func TestDataBrSerializaEmBrasiliaMesmoRecebendoUTC(t *testing.T) {

	// Exatamente o que o pgx entregaria num container sem TZ para a linha que o
	// banco guarda como 2026-08-31 21:56:51-03.
	doPgx := time.Date(2026, 9, 1, 0, 56, 51, 0, time.UTC)

	bruto, err := json.Marshal(NewDataBrPtr(doPgx))
	if err != nil {
		t.Fatalf("erro ao serializar: %v", err)
	}

	if string(bruto) != `"31/08/2026 21:56:51"` {
		t.Errorf("data = %s, esperado \"31/08/2026 21:56:51\"", bruto)
	}
}

// A ida tem que assumir o mesmo fuso da volta: com time.Parse (que assume UTC)
// o texto de 21:56 virava 18:56 de Brasília, e o round trip não fechava.
func TestDataBrRoundTrip(t *testing.T) {

	casos := []string{`"31/08/2026 21:56:51"`, `"01/01/2026 00:00:00"`}

	for _, caso := range casos {
		var d DataBr
		if err := d.UnmarshalJSON([]byte(caso)); err != nil {
			t.Fatalf("erro ao desserializar %s: %v", caso, err)
		}
		bruto, err := d.MarshalJSON()
		if err != nil {
			t.Fatalf("erro ao serializar %s: %v", caso, err)
		}
		if string(bruto) != caso {
			t.Errorf("round trip de %s devolveu %s", caso, bruto)
		}
	}
}

// Coluna `date` chega sem hora (preventiva.proxima_data). O dia não pode
// escorregar: parseado em UTC e formatado em Brasília, 01/09 virava 31/08.
func TestDataBrSemHoraMantemODia(t *testing.T) {

	var d DataBr
	if err := d.UnmarshalJSON([]byte(`"01/09/2026"`)); err != nil {
		t.Fatalf("erro ao desserializar: %v", err)
	}

	bruto, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("erro ao serializar: %v", err)
	}
	if string(bruto) != `"01/09/2026 00:00:00"` {
		t.Errorf("data = %s, esperado \"01/09/2026 00:00:00\"", bruto)
	}
}
