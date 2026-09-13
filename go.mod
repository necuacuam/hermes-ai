module hermes-mcp

go 1.18

// go.mongodb.org/mongo-driver: unica dependencia externa del proyecto,
// necesaria para internal/store/mongostore. No se pudo bajar ni resolver
// en el entorno donde se escribio este modulo (go.mongodb.org bloqueado en
// esa red) -- correr `go mod tidy` en una maquina con internet normal para
// que se resuelva la version exacta y se genere go.sum. v1.13.1 es la que
// se puso como punto de partida (compatible con go 1.18); tidy puede subirla.
require go.mongodb.org/mongo-driver v1.13.1

require (
	github.com/golang/snappy v0.0.1 // indirect
	github.com/klauspost/compress v1.13.6 // indirect
	github.com/montanaflynn/stats v0.0.0-20171201202039-1bf9dbcd8cbe // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20181117223130-1be2e3e5546d // indirect
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d // indirect
	golang.org/x/sync v0.0.0-20220722155255-886fb9371eb4 // indirect
	golang.org/x/text v0.7.0 // indirect
)
