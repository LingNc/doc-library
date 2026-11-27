package doclibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type frequencyEntry struct {
	Result  Result `json:"result"`
	Digest  string `json:"digest"`
	Updated int64  `json:"updated"`
}
type frequencyStore struct {
	Schema  string           `json:"schema"`
	Entries []frequencyEntry `json:"entries"`
}

func entryID(source, doc string, page, start, end int) string {
	return hashBytes([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", source, doc, page, start, end)))
}
func (l *Library) frequencyEnabled() bool {
	return l.config.CacheMode != "off" && (l.config.Frequency.Enabled == nil || *l.config.Frequency.Enabled)
}
func (l *Library) readFrequency() (frequencyStore, []string, error) {
	var st frequencyStore
	b, e := os.ReadFile(l.cachePath("frequency-v1.json"))
	if os.IsNotExist(e) {
		return st, nil, nil
	}
	if e != nil {
		return st, nil, e
	}
	if json.Unmarshal(b, &st) != nil || st.Schema != extractionSchema {
		return frequencyStore{}, []string{"frequency cache was invalid; starting with an empty history"}, nil
	}
	return st, nil, nil
}

// recordFrequency retains the old canonical paragraph identity and active/passive
// distinction, while updating all emitted entries in one cross-process transaction.
func (l *Library) recordFrequency(ctx context.Context, entries []frequencyEntry) ([]string, error) {
	if !l.frequencyEnabled() || len(entries) == 0 {
		return nil, nil
	}
	release, e := acquireLock(ctx, l.cachePath("frequency.lock"))
	if e != nil {
		return nil, e
	}
	defer release()
	st, warnings, e := l.readFrequency()
	if e != nil {
		return nil, e
	}
	st.Schema = extractionSchema
	for _, incoming := range entries {
		if incoming.Result.Text == "" {
			continue
		}
		incoming.Result.Text = truncate(incoming.Result.Text, l.config.Frequency.TextChars)
		incoming.Result.EntryID = entryID(incoming.Result.Source, incoming.Result.Doc, incoming.Result.Page, incoming.Result.StartLine, incoming.Result.EndLine)
		incoming.Updated = time.Now().Unix()
		found := false
		for i := range st.Entries {
			old := &st.Entries[i]
			if old.Result.EntryID == incoming.Result.EntryID {
				if old.Digest == incoming.Digest {
					incoming.Result.Hits = old.Result.Hits + 1
				} else {
					incoming.Result.Hits = 1
				}
				*old = incoming
				found = true
				break
			}
		}
		if !found {
			incoming.Result.Hits = 1
			st.Entries = append(st.Entries, incoming)
		}
	}
	sortFrequency(st.Entries)
	if len(st.Entries) > l.config.Frequency.MaxEntries {
		st.Entries = st.Entries[:l.config.Frequency.MaxEntries]
	}
	if e = atomicJSON(l.cachePath("frequency-v1.json"), st); e != nil {
		return warnings, e
	}
	return warnings, nil
}
func sortFrequency(es []frequencyEntry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].Result.Hits != es[j].Result.Hits {
			return es[i].Result.Hits > es[j].Result.Hits
		}
		return es[i].Result.EntryID < es[j].Result.EntryID
	})
}
func (l *Library) frequent(ctx context.Context, r Request, sources []Source, paging *pageState) ([]Result, []frequencyEntry, []string, error) {
	if !l.frequencyEnabled() {
		return nil, nil, []string{"frequency is disabled (cache_mode=off or frequency.enabled=false)"}, nil
	}
	st, warnings, e := l.readFrequency()
	if e != nil {
		return nil, nil, warnings, e
	}
	selected := map[string]Source{}
	for _, s := range sources {
		selected[s.ID] = s
	}
	digests := map[string]string{}
	var valid []frequencyEntry
	for _, entry := range st.Entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, warnings, err
		}
		s, ok := selected[entry.Result.Source]
		if !ok {
			continue
		}
		if r.Doc != "" && entry.Result.Doc != r.Doc {
			continue
		}
		if !allowed(s, entry.Result.Doc) || !requestAllows(r, entry.Result.Doc) || (r.Page > 0 && entry.Result.Page != r.Page) {
			continue
		}
		k := s.ID + "\x00" + entry.Result.Doc
		digest, ok := digests[k]
		if !ok {
			b, err := l.readFile(s, entry.Result.Doc)
			if err != nil {
				if safeError(err) {
					continue
				}
				return nil, nil, warnings, err
			}
			digest = hashBytes(b)
			digests[k] = digest
		}
		if digest != entry.Digest {
			continue
		}
		if r.EntryID != "" && !strings.HasPrefix(entry.Result.EntryID, r.EntryID) {
			continue
		}
		valid = append(valid, entry)
	}
	if r.EntryID != "" && len(valid) != 1 {
		return nil, nil, warnings, fmt.Errorf("entry_id must identify exactly one current entry; matched %d", len(valid))
	}
	sortFrequency(valid)
	start := 0
	end := len(valid)
	if r.StartLine > 0 || r.EndLine > 0 {
		if r.Doc == "" {
			start = maxInt(0, r.StartLine-1)
			if r.EndLine > 0 {
				end = minInt(end, r.EndLine)
			}
		} else {
			filtered := valid[:0]
			for _, v := range valid {
				if (r.StartLine == 0 || v.Result.EndLine >= r.StartLine) && (r.EndLine == 0 || v.Result.StartLine <= r.EndLine) {
					filtered = append(filtered, v)
				}
			}
			valid = filtered
			end = len(valid)
		}
	}
	if start >= end {
		return nil, nil, warnings, nil
	}
	p := pageCollector{r: r, state: paging}
	for _, entry := range valid[start:end] {
		result := entry.Result
		chars := r.LineChars
		if chars == 0 {
			chars = 256
		}
		original := strings.Join(strings.Fields(result.Text), " ")
		result.Text = truncate(original, chars)
		result.Truncated = len([]rune(result.Text)) < len([]rune(original))
		if !p.add(candidate{result: result, records: []frequencyEntry{entry}}) {
			break
		}
	}
	return p.results, p.records, warnings, nil
}
