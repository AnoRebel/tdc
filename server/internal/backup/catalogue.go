package backup

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is what both a pool and a transaction offer.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// column is one column a restore writes. Generated columns are left out:
// Postgres computes them and refuses a value for one.
type column struct {
	name string
}

/*
reference is a single-column foreign key.

required is whether a row is meaningless without its target — the reference
cascades or restricts on delete — so a restore skips a row whose target is
missing. Otherwise the reference is optional (it is set null when its target
goes), and a restore clears it instead.
*/
type reference struct {
	column    string
	table     string
	refColumn string
	required  bool
}

// tableInfo is what a restore needs to know about one table.
type tableInfo struct {
	name       string
	columns    []column
	primaryKey []string
	references []reference
	// serials are columns filled from a sequence, which a restore moves past
	// the highest value it wrote.
	serials []string
}

// selfReferences are the references a table makes to itself, which are
// written in a second pass so a row never waits on one later in the stream.
func (t *tableInfo) selfReferences() []reference {
	var found []reference
	for _, ref := range t.references {
		if ref.table == t.name {
			found = append(found, ref)
		}
	}
	return found
}

// readCatalogue describes every table in the current schema.
func readCatalogue(ctx context.Context, q querier) (map[string]*tableInfo, error) {
	tables := map[string]*tableInfo{}

	rows, err := q.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relkind IN ('r', 'p')`)
	if err != nil {
		return nil, fmt.Errorf("backup: listing tables: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("backup: listing tables: %w", err)
	}
	for _, name := range names {
		tables[name] = &tableInfo{name: name}
	}

	rows, err = q.Query(ctx, `
		SELECT table_name, column_name, is_generated = 'ALWAYS',
		       COALESCE(column_default LIKE 'nextval(%', FALSE)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("backup: reading columns: %w", err)
	}
	for rows.Next() {
		var table, name string
		var generated, serial bool
		if err := rows.Scan(&table, &name, &generated, &serial); err != nil {
			rows.Close()
			return nil, fmt.Errorf("backup: reading columns: %w", err)
		}
		info, ok := tables[table]
		if !ok || generated {
			continue
		}
		info.columns = append(info.columns, column{name: name})
		if serial {
			info.serials = append(info.serials, name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("backup: reading columns: %w", err)
	}

	rows, err = q.Query(ctx, `
		SELECT cl.relname, a.attname
		FROM pg_constraint c
		JOIN pg_class cl ON cl.oid = c.conrelid
		JOIN LATERAL unnest(c.conkey) WITH ORDINALITY AS k(attnum, n) ON TRUE
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		WHERE c.contype = 'p' AND cl.relnamespace = current_schema()::regnamespace
		ORDER BY cl.relname, k.n`)
	if err != nil {
		return nil, fmt.Errorf("backup: reading keys: %w", err)
	}
	for rows.Next() {
		var table, name string
		if err := rows.Scan(&table, &name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("backup: reading keys: %w", err)
		}
		if info, ok := tables[table]; ok {
			info.primaryKey = append(info.primaryKey, name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("backup: reading keys: %w", err)
	}

	// Composite references are not handled, and the schema has none; one
	// appearing would make a restore fail on its constraint, loudly, rather
	// than write rows that point at nothing.
	rows, err = q.Query(ctx, `
		SELECT cl.relname, a.attname, rc.relname, ra.attname, c.confdeltype NOT IN ('n', 'd')
		FROM pg_constraint c
		JOIN pg_class cl ON cl.oid = c.conrelid
		JOIN pg_class rc ON rc.oid = c.confrelid
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		JOIN pg_attribute ra ON ra.attrelid = c.confrelid AND ra.attnum = c.confkey[1]
		WHERE c.contype = 'f' AND array_length(c.conkey, 1) = 1
		  AND cl.relnamespace = current_schema()::regnamespace
		ORDER BY cl.relname, a.attname`)
	if err != nil {
		return nil, fmt.Errorf("backup: reading references: %w", err)
	}
	for rows.Next() {
		var ref reference
		var table string
		if err := rows.Scan(&table, &ref.column, &ref.table, &ref.refColumn, &ref.required); err != nil {
			rows.Close()
			return nil, fmt.Errorf("backup: reading references: %w", err)
		}
		if info, ok := tables[table]; ok {
			info.references = append(info.references, ref)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("backup: reading references: %w", err)
	}

	return tables, nil
}

/*
ordered returns tables so that each comes after every table it references.

Ties are broken by part, then by the order partTables lists them, so the order
is the same on every run and an archive reads in the order a person would
expect. Self-references are ignored here and handled by a second pass.
*/
func ordered(names []string, catalogue map[string]*tableInfo) ([]string, error) {
	position := map[string]int{}
	for i, part := range Parts {
		for j, spec := range partTables[part] {
			position[spec.name] = i*1000 + j
		}
	}

	inSet := map[string]bool{}
	for _, name := range names {
		inSet[name] = true
	}

	waiting := map[string]int{}
	dependents := map[string][]string{}
	for _, name := range names {
		info, ok := catalogue[name]
		if !ok {
			return nil, fmt.Errorf("backup: table %s does not exist", name)
		}
		seen := map[string]bool{}
		for _, ref := range info.references {
			if ref.table == name || !inSet[ref.table] || seen[ref.table] {
				continue
			}
			seen[ref.table] = true
			waiting[name]++
			dependents[ref.table] = append(dependents[ref.table], name)
		}
	}

	var ready, result []string
	for _, name := range names {
		if waiting[name] == 0 {
			ready = append(ready, name)
		}
	}
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return position[ready[i]] < position[ready[j]] })
		next := ready[0]
		ready = ready[1:]
		result = append(result, next)
		for _, dependent := range dependents[next] {
			waiting[dependent]--
			if waiting[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}

	if len(result) != len(names) {
		return nil, fmt.Errorf("backup: the tables reference each other in a cycle")
	}
	return result, nil
}

func quote(name string) string { return pgx.Identifier{name}.Sanitize() }
