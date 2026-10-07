package mongostore

import "testing"

func TestURISinClave(t *testing.T) {
	casos := map[string]string{
		"mongodb://hermes:secreto@localhost:27017/?authSource=admin": "mongodb://hermes:****@localhost:27017/?authSource=admin",
		"mongodb+srv://usr:p%40ss@cluster.example.net/db":            "mongodb+srv://usr:****@cluster.example.net/db",
		"mongodb://usr:clave@h1:27017,h2:27017/?replicaSet=rs0":      "mongodb://usr:****@h1:27017,h2:27017/?replicaSet=rs0",
		"mongodb://localhost:27017":                                  "mongodb://localhost:27017",
		"mongodb://soloUsuario@localhost:27017/?authMechanism=X":     "mongodb://soloUsuario@localhost:27017/?authMechanism=X",
	}
	for uri, want := range casos {
		if got := URISinClave(uri); got != want {
			t.Errorf("URISinClave(%q) = %q, se esperaba %q", uri, got, want)
		}
	}
}
