// Package mockstore es una implementacion en memoria de store.Store, para
// poder desarrollar y probar el servidor MCP sin acceso a Mongo de UAT.
// Los datos vienen de fixtures.go -- documentos inventados a mano que
// representan el ejemplo de siempre (cliente Macedonia / sucursal 23 /
// sala 3 / camaras) mas un par de casos adicionales para no probar contra
// un unico caso feliz.
package mockstore

import (
	"context"

	"hermes-mcp/internal/domain"
)

type MockStore struct {
	docs []domain.Document
}

func New(docs []domain.Document) *MockStore {
	return &MockStore{docs: docs}
}

func (m *MockStore) FindByNameAndType(_ context.Context, needle string, t domain.EntityType) ([]domain.Document, error) {
	var out []domain.Document
	for _, d := range m.docs {
		if d.Type == t && domain.ContainsFold(d.Name, needle) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *MockStore) FindChildren(_ context.Context, parentUCode string) ([]domain.Document, error) {
	var out []domain.Document
	for _, d := range m.docs {
		if d.ParentUCode == parentUCode {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *MockStore) Get(_ context.Context, ucode string) (domain.Document, bool, error) {
	for _, d := range m.docs {
		if d.UCode == ucode {
			return d, true, nil
		}
	}
	return domain.Document{}, false, nil
}
