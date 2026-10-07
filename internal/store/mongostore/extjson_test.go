package mongostore

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// pipelineDeJSON arma el pipeline como lo recibe reporting: una etapa por
// json.RawMessage, con el texto original.
func pipelineDeJSON(t *testing.T, s string) []json.RawMessage {
	t.Helper()
	var p []json.RawMessage
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return p
}

func TestPipelineDesdeExtJSON_ConservaOrdenDeClaves(t *testing.T) {
	// Caso real (2026-10-03): ranking por puntos con desempate por
	// centroUcode. En orden alfabetico, centroUcode quedaba primero.
	p := pipelineDeJSON(t, `[{"$sort": {"puntos": -1, "centroUcode": 1}}]`)

	out, err := pipelineDesdeExtJSON(p)
	if err != nil {
		t.Fatalf("pipelineDesdeExtJSON: %v", err)
	}
	sort := out[0][0].Value.(bson.D)
	if len(sort) != 2 || sort[0].Key != "puntos" || sort[1].Key != "centroUcode" {
		t.Errorf("orden del $sort = %v, se esperaba [puntos centroUcode]", sort)
	}
}

func TestPipelineDesdeExtJSON_UUID(t *testing.T) {
	const agentID = "e5b34837-9530-e711-812c-e0071b6641d1"
	p := pipelineDeJSON(t, `[{"$match": {"uuid": {"$in": [{"$uuid": "`+agentID+`"}]}}}]`)

	out, err := pipelineDesdeExtJSON(p)
	if err != nil {
		t.Fatalf("pipelineDesdeExtJSON: %v", err)
	}
	in := out[0].Map()["$match"].(bson.D).Map()["uuid"].(bson.D).Map()["$in"].(bson.A)
	bin, ok := in[0].(primitive.Binary)
	if !ok {
		t.Fatalf("se esperaba primitive.Binary, quedo %T", in[0])
	}
	want, _ := hex.DecodeString("e5b348379530e711812ce0071b6641d1")
	if bin.Subtype != 4 || !bytes.Equal(bin.Data, want) {
		t.Errorf("uuid = subtype %d %x, se esperaba subtype 4 %x", bin.Subtype, bin.Data, want)
	}
}

func TestPipelineDesdeExtJSON_SinTiposEspeciales(t *testing.T) {
	p := pipelineDeJSON(t, `[{"$match": {"data.accountName": {"$regex": "Santander", "$options": "i"}}}, {"$limit": 10}]`)

	out, err := pipelineDesdeExtJSON(p)
	if err != nil {
		t.Fatalf("pipelineDesdeExtJSON: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("se esperaban 2 etapas, quedaron %d", len(out))
	}
	m := out[0].Map()["$match"].(bson.D).Map()["data.accountName"].(bson.D).Map()
	if m["$regex"] != "Santander" || m["$options"] != "i" {
		t.Errorf("$match cambio al convertir: %v", m)
	}
}

func TestPipelineDesdeExtJSON_UUIDInvalido(t *testing.T) {
	p := pipelineDeJSON(t, `[{"$match": {"uuid": {"$uuid": "no-es-un-uuid"}}}]`)
	if _, err := pipelineDesdeExtJSON(p); err == nil {
		t.Fatal("se esperaba error para un $uuid invalido")
	}
}
