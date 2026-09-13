// Package graph implementa la tool "Relacion entre entidades": resolver una
// cadena de nombres de negocio (cliente -> centro -> ... -> centro) contra
// la jerarquia real, y contar/listar descendientes.
//
// Decision de diseno importante (anidamiento): el codigo actual de Hermes
// (handle-create-code.go) fuerza que el ParentUCode de un centro apunte
// siempre a un cliente, y el de un punto siempre a un centro -- rechaza
// centro-en-centro y punto-en-punto. Pero la documentacion de negocio
// (Norma General 3P) describe explicitamente esa posibilidad como
// funcionalidad prevista ("se habilitara la posibilidad de que un centro
// dependa de otro"), con ejemplos reales (Bernabeu subdividido en zonas,
// rack conteniendo paneles).
//
// CONFIRMADO CONTRA UAT (2026-09-08): 0 servicePlace con padre-servicePlace
// sobre 20072, y 0 servicePoint con padre-servicePoint sobre 13976 -- el
// anidamiento descrito en la Norma 3P no esta en uso en datos reales hoy;
// el codigo (que lo rechaza) es el que refleja la realidad actual.
//
// Aun asi, este paquete sigue sin asumir ninguna de las dos lecturas:
// camina el arbol siguiendo ParentUCode de forma puramente estructural, sin
// mirar el tipo del padre ni del hijo. Si el dia de mañana Hermes habilita
// el anidamiento, este traversal sigue funcionando sin cambios.
package graph

import (
	"context"
	"fmt"
	"strings"

	"hermes-mcp/internal/domain"
	"hermes-mcp/internal/store"
)

type Graph struct {
	Store store.Store
}

func New(s store.Store) *Graph {
	return &Graph{Store: s}
}

// PathStep es un tramo resuelto de la cadena pedida -- se devuelve toda la
// cadena (no solo el ultimo nodo) para que el agente pueda mostrarle al
// usuario, si hace falta, por donde se resolvio la pregunta (por ejemplo,
// para detectar sorpresas: "ah, resulta que Sala 3 es un centro anidado,
// no un punto").
type PathStep struct {
	Query string          `json:"consulta"`  // el nombre que se pidio buscar
	Doc   domain.Document `json:"documento"` // el documento encontrado
}

// ResolvePath busca, en orden, un cliente por nombre y luego, para cada
// nombre siguiente, un descendiente DIRECTO (hijo) cuyo nombre matchee --
// sin importar su tipo (podria ser un centro o, si el anidamiento aplica,
// otro centro mas adentro). Si algun nombre no matchea ningun hijo directo,
// devuelve error indicando en que paso se corto.
//
// clienteNombre es obligatorio; centrosNombres es la lista de nombres a
// bajar despues del cliente, en orden (puede ser vacia si la pregunta es
// directamente sobre el cliente).
func (g *Graph) ResolvePath(ctx context.Context, clienteNombre string, centrosNombres []string) ([]PathStep, error) {
	clientes, err := g.Store.FindByNameAndType(ctx, clienteNombre, domain.Cliente)
	if err != nil {
		return nil, err
	}
	if len(clientes) == 0 {
		return nil, fmt.Errorf("no se encontro ningun cliente cuyo nombre contenga %q", clienteNombre)
	}
	if len(clientes) > 1 {
		return nil, fmt.Errorf("%q es ambiguo -- matchea %d clientes distintos, hace falta mas precision", clienteNombre, len(clientes))
	}

	steps := []PathStep{{Query: clienteNombre, Doc: clientes[0]}}
	current := clientes[0]

	for _, nombre := range centrosNombres {
		children, err := g.Store.FindChildren(ctx, current.UCode)
		if err != nil {
			return nil, err
		}
		var matches []domain.Document
		for _, c := range children {
			if domain.ContainsFold(c.Name, nombre) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 0 {
			return steps, fmt.Errorf("no se encontro %q como hijo directo de %q (ucode %s)", nombre, current.Name, current.UCode)
		}
		if len(matches) > 1 {
			return steps, fmt.Errorf("%q es ambiguo bajo %q -- matchea %d hijos", nombre, current.Name, len(matches))
		}
		current = matches[0]
		steps = append(steps, PathStep{Query: nombre, Doc: current})
	}

	return steps, nil
}

// SearchChildren busca, entre los hijos DIRECTOS de padreUCode, los que
// matchean needle (sin distinguir mayusculas/acentos) y, si t no es vacio,
// ademas son del tipo t. Si needle es "" devuelve TODOS los hijos directos
// (sin filtrar por nombre) -- es el modo "listar" para cuando el usuario no
// tiene ninguna pista de nombre y necesita ver las opciones para reconocer
// la que busca, en vez de tener que adivinar un texto para buscar.
//
// A proposito NO devuelve error si hay 0 o varios matches -- la ambiguedad
// (o la ausencia de match) es informacion valida que el llamador (el
// agente) tiene que poder mostrarle al usuario para que elija, no un fallo.
func (g *Graph) SearchChildren(ctx context.Context, padreUCode, needle string, t domain.EntityType) ([]domain.Document, error) {
	children, err := g.Store.FindChildren(ctx, padreUCode)
	if err != nil {
		return nil, err
	}
	var out []domain.Document
	for _, c := range children {
		if t != "" && c.Type != t {
			continue
		}
		if needle != "" && !domain.ContainsFold(c.Name, needle) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// SearchGlobal busca needle entre TODOS los tipos de entidad (o solo en t,
// si no es vacio), sin asumir ningun nivel de la jerarquia. Pensado para el
// primer paso de una resolucion, cuando todavia no se sabe si lo que el
// usuario nombro es un cliente, un centro, etc. -- o si hay mas de una
// entidad con un nombre parecido en ramas distintas del arbol.
func (g *Graph) SearchGlobal(ctx context.Context, needle string, t domain.EntityType) ([]domain.Document, error) {
	types := domain.AllTypes
	if t != "" {
		types = []domain.EntityType{t}
	}
	var out []domain.Document
	for _, typ := range types {
		docs, err := g.Store.FindByNameAndType(ctx, needle, typ)
		if err != nil {
			return nil, err
		}
		out = append(out, docs...)
	}
	return out, nil
}

// Breadcrumb arma una cadena legible ("Macedonia > Sucursal 23 (Madrid)")
// subiendo por ParentUCode desde doc hasta la raiz, para que un humano
// pueda reconocer un candidato ambiguo sin tener que interpretar ucodes.
// Corta silenciosamente ante datos inconsistentes (padre inexistente o
// ciclo) en vez de fallar -- el breadcrumb es una ayuda visual, no una
// fuente de verdad.
func (g *Graph) Breadcrumb(ctx context.Context, doc domain.Document) string {
	parts := []string{doc.Name}
	seen := map[string]bool{doc.UCode: true}
	current := doc
	for current.ParentUCode != "" {
		parent, found, err := g.Store.Get(ctx, current.ParentUCode)
		if err != nil || !found || seen[parent.UCode] {
			break
		}
		parts = append([]string{parent.Name}, parts...)
		seen[parent.UCode] = true
		current = parent
	}
	return strings.Join(parts, " > ")
}

// AllDescendants devuelve TODOS los descendientes de rootUCode, recorriendo
// la jerarquia recursivamente sin limite de profundidad (a proposito: ver
// la nota del paquete sobre por que no se asume una profundidad fija).
func (g *Graph) AllDescendants(ctx context.Context, rootUCode string) ([]domain.Document, error) {
	var out []domain.Document
	queue := []string{rootUCode}
	seen := map[string]bool{}

	for len(queue) > 0 {
		ucode := queue[0]
		queue = queue[1:]
		if seen[ucode] {
			continue // proteccion basica contra un ciclo mal formado en los datos
		}
		seen[ucode] = true

		children, err := g.Store.FindChildren(ctx, ucode)
		if err != nil {
			return nil, err
		}
		for _, c := range children {
			out = append(out, c)
			queue = append(queue, c.UCode)
		}
	}
	return out, nil
}

// CountByType cuenta, entre todos los descendientes de rootUCode, cuantos
// son del tipo t y ademas (si attrKey no es vacio) tienen ese atributo
// conteniendo attrNeedle. Devuelve tambien la lista completa que matcheo,
// para que el llamador decida si escribir el detalle a archivo.
func (g *Graph) CountByType(ctx context.Context, rootUCode string, t domain.EntityType, attrKey, attrNeedle string) ([]domain.Document, error) {
	all, err := g.AllDescendants(ctx, rootUCode)
	if err != nil {
		return nil, err
	}
	var out []domain.Document
	for _, d := range all {
		if d.Type != t {
			continue
		}
		if attrKey != "" && !d.AttributeContainsFold(attrKey, attrNeedle) {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}
