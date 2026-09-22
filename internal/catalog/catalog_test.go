package catalog

import "testing"

func TestAllIsolatedCopy(t *testing.T) {
	first := All()
	if len(first) != len(groups) {
		t.Fatalf("All() вернул %d групп, ожидалось %d", len(first), len(groups))
	}

	// Копия обязана быть независимой: правка возвращённого среза не должна
	// менять сам каталог — иначе один вызов портит данные всем остальным.
	first[0].Domains[0] = "испорчено.example"
	first[0].Title = "испорчено"

	second := All()
	if second[0].Domains[0] != groups[0].Domains[0] || second[0].Title != groups[0].Title {
		t.Fatalf("All() отдаёт ссылку на внутренние данные: %+v", second[0])
	}
}

func TestAllDomainsAreFilled(t *testing.T) {
	for _, g := range All() {
		if g.ID == "" || g.Title == "" || g.Icon == "" {
			t.Errorf("у группы не заполнены обязательные поля: %+v", g)
		}
		if len(g.Domains) == 0 {
			t.Errorf("у группы %q нет доменов", g.ID)
		}
	}
}

func TestGroupsByID(t *testing.T) {
	got := GroupsByID([]string{"video", "social"})
	// Ожидание собирается в порядке каталога: GroupsByID не переставляет группы
	// под порядок запроса.
	var want []string
	for _, g := range groups {
		if g.ID == "video" || g.ID == "social" {
			want = append(want, g.Domains...)
		}
	}
	if !equal(got, want) {
		t.Fatalf("GroupsByID() вернул %d доменов, ожидалось %d", len(got), len(want))
	}
	if len(got) == 0 {
		t.Fatal("список доменов пуст")
	}
}

func TestGroupsByIDIgnoresUnknown(t *testing.T) {
	if got := GroupsByID([]string{"нет-такой-группы"}); len(got) != 0 {
		t.Fatalf("неизвестная группа дала домены: %v", got)
	}
	if got := GroupsByID(nil); len(got) != 0 {
		t.Fatalf("пустой запрос дал домены: %v", got)
	}
	// Порядок — как в каталоге, а не как в запросе: иначе содержимое политики
	// браузеров зависело бы от порядка галочек в интерфейсе.
	if !equal(GroupsByID([]string{"social", "video"}), GroupsByID([]string{"video", "social"})) {
		t.Fatal("порядок групп в запросе не должен влиять на результат")
	}
}

func TestGroupsByIDReturnsEveryRequestedDomain(t *testing.T) {
	for _, g := range All() {
		if !equal(GroupsByID([]string{g.ID}), g.Domains) {
			t.Errorf("группа %q вернулась не целиком", g.ID)
		}
	}
}

func TestDoHIsIsolatedCopy(t *testing.T) {
	got := DoH()
	if !equal(got, dohProviders) {
		t.Fatalf("DoH() = %v, ожидалось %v", got, dohProviders)
	}
	got[0] = "испорчено.example"
	if DoH()[0] != dohProviders[0] {
		t.Fatal("DoH() отдаёт ссылку на внутренний срез")
	}
}

func TestDefaultsAreKnownGroups(t *testing.T) {
	def := Defaults()
	if len(def) == 0 {
		t.Fatal("список групп по умолчанию пуст")
	}
	known := make(map[string]bool)
	for _, id := range IDs() {
		known[id] = true
	}
	for _, id := range def {
		if !known[id] {
			t.Errorf("группа по умолчанию %q отсутствует в каталоге", id)
		}
	}
}

func TestIDsOrderMatchesCatalog(t *testing.T) {
	ids := IDs()
	if len(ids) != len(groups) {
		t.Fatalf("IDs() вернул %d идентификаторов, ожидалось %d", len(ids), len(groups))
	}
	for i, g := range groups {
		if ids[i] != g.ID {
			t.Fatalf("IDs()[%d] = %q, ожидалось %q", i, ids[i], g.ID)
		}
	}
	if len(IDs()) == 0 {
		t.Fatal("каталог пуст")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
