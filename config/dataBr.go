package config

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

type DataBr time.Time

func (d *DataBr) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "null" || s == "" {
		return nil
	}
	// Layout brasileiro com data e hora: DD/MM/YYYY HH:MM:SS
	t, err := time.Parse("02/01/2006 15:04:05", s)
	if err != nil {
		// Sem hora: o contrato aceita as duas formas, e campo de coluna `date`
		// chega assim -- preventiva.proxima_data vem de um <input type="date">
		// que o front converte para dd/mm/yyyy antes de enviar. Sem este
		// fallback, cadastrar preventiva falhava na borda, antes do service.
		t, err = time.Parse("02/01/2006", s)
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
	return fmt.Appendf(nil, "\"%s\"", t.Format("02/01/2006 15:04:05")), nil
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
