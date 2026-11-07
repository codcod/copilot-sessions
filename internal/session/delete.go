package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// dbSessionTables are the tables keyed by session_id that Delete prunes when they exist.
var dbSessionTables = []string{
	"search_index", "turns", "checkpoints", "session_files", "session_refs",
	"forge_trajectory_events", "assistant_usage_events",
}

// Delete removes ss: their directories, DB rows and UI-state entries (spec §6.2).
// With everything set, the UI state is cleared completely.
func Delete(home string, ss []Session, everything bool) error {
	ids := map[string]bool{}
	for _, s := range ss {
		ids[s.ID] = true
		if s.Dir != "" {
			os.RemoveAll(s.Dir) // errors ignored, like shutil.rmtree(ignore_errors=True)
		}
	}
	if err := pruneDB(home, ids); err != nil {
		return err
	}
	return pruneUIState(home, ids, everything)
}

func pruneDB(home string, ids map[string]bool) error {
	db, err := OpenDB(home)
	if db == nil || err != nil || len(ids) == 0 {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("prune %s: %w", DBFile, err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) error {
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("prune %s: %w", DBFile, err)
		}
		return nil
	}
	if err := exec("CREATE TEMP TABLE prune_ids (id TEXT PRIMARY KEY)"); err != nil {
		return err
	}
	for id := range ids {
		if err := exec("INSERT OR IGNORE INTO prune_ids VALUES (?)", id); err != nil {
			return err
		}
	}
	rows, err := tx.Query("SELECT name FROM sqlite_master")
	if err != nil {
		return fmt.Errorf("prune %s: %w", DBFile, err)
	}
	existing := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("prune %s: %w", DBFile, err)
		}
		existing[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("prune %s: %w", DBFile, err)
	}
	for _, t := range dbSessionTables { // names come only from the constant list
		if existing[t] {
			if err := exec("DELETE FROM " + t + " WHERE session_id IN (SELECT id FROM prune_ids)"); err != nil {
				return err
			}
		}
	}
	if err := exec("DELETE FROM sessions WHERE id IN (SELECT id FROM prune_ids)"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("prune %s: %w", DBFile, err)
	}
	return nil
}

func pruneUIState(home string, ids map[string]bool, everything bool) error {

	open := filepath.Join(home, OpenState)
	if fi, err := os.Stat(open); err == nil && fi.Mode().IsRegular() {
		data, err := readJSONFile(open)
		if err != nil {
			return err
		}
		n := len(data)
		for k := range data {
			if everything || ids[k] {
				delete(data, k)
			}
		}
		if len(data) != n {
			if err := writeJSONFile(open, data); err != nil {
				return err
			}
		}
	}

	files, _ := filepath.Glob(filepath.Join(home, SidebarDir, "*.json"))
	for _, f := range files {
		data, err := readJSONFile(f)
		if err != nil {
			return err
		}
		var sids []json.RawMessage
		if raw, ok := data["sessionIds"]; !ok || json.Unmarshal(raw, &sids) != nil || sids == nil {
			continue // missing or not an array: left alone
		}
		kept := []json.RawMessage{}
		for _, raw := range sids {
			var id string
			if everything || json.Unmarshal(raw, &id) == nil && ids[id] {
				continue
			}
			kept = append(kept, raw)
		}
		if len(kept) != len(sids) {
			data["sessionIds"], _ = json.Marshal(kept)
			if err := writeJSONFile(f, data); err != nil {
				return err
			}
		}
	}
	return nil
}

// readJSONFile decodes a JSON object file; an empty file reads as {}.
func readJSONFile(path string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(b)) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if data == nil {
		data = map[string]json.RawMessage{}
	}
	return data, nil
}

// writeJSONFile writes v with 2-space indent and a trailing newline, atomically, keeping
// the existing file's permissions.
func writeJSONFile(path string, v any) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	err = enc.Encode(v)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), fi.Mode().Perm())
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	return err
}
