package semantic

import (
	"reflect"
	"testing"
)

func TestNewFactIDIsStableAndLocationSensitive(t *testing.T) {
	left := NewFact(FactSource, "http.query", Location{File: "handler.go", StartLine: 7, StartColumn: 3})
	right := NewFact(FactSource, "http.query", Location{File: "handler.go", StartLine: 7, StartColumn: 3})
	other := NewFact(FactSource, "http.query", Location{File: "handler.go", StartLine: 8, StartColumn: 3})

	if left.ID == "" || left.ID != right.ID {
		t.Fatalf("stable IDs = %q and %q", left.ID, right.ID)
	}
	if left.ID == other.ID {
		t.Fatalf("different locations shared ID %q", left.ID)
	}
}

func TestIndexSortsFactsAndDetectsEcosystems(t *testing.T) {
	documents := []*Document{
		{Path: "z.go", Language: "Go", Imports: []string{"gorm.io/gorm", "github.com/gofiber/fiber/v3"}, Facts: []Fact{
			{ID: "z", Kind: FactSink, Operation: "sql.raw", Location: Location{File: "z.go", StartLine: 8}},
		}},
		{Path: "a.go", Language: "Go", Imports: []string{"github.com/gin-gonic/gin", "net/http"}, Facts: []Fact{
			{ID: "a2", Kind: FactSink, Operation: "command.exec", Location: Location{File: "a.go", StartLine: 9}},
			{ID: "a1", Kind: FactSink, Operation: "sql.exec", Location: Location{File: "a.go", StartLine: 2}},
		}},
	}

	index := NewIndex(documents)
	for _, ecosystem := range []Ecosystem{EcosystemGin, EcosystemFiber, EcosystemGORM, EcosystemNetHTTP} {
		if !index.HasEcosystem(ecosystem) {
			t.Errorf("expected %s to be detected", ecosystem)
		}
	}
	got := index.Facts(FactSink)
	want := []string{"a.go:2:sql.exec", "a.go:9:command.exec", "z.go:8:sql.raw"}
	keys := make([]string, 0, len(got))
	for _, fact := range got {
		keys = append(keys, fact.Location.File+":"+itoa(fact.Location.StartLine)+":"+fact.Operation)
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("fact order = %v, want %v", keys, want)
	}
}

func TestIndexDetectsReleaseLanguageEcosystems(t *testing.T) {
	index := NewIndex([]*Document{{Path: "all", Imports: []string{"flask.request", "fastapi.Query", "django.http", "sqlalchemy", "torch", "cv2", "ultralytics.YOLO", "Microsoft.AspNetCore.Http", "Microsoft.EntityFrameworkCore", "tokio::task", "axum::extract::Query", "actix_web::web", "sqlx", "diesel", "reqwest", "tonic"}}})
	for _, ecosystem := range []Ecosystem{EcosystemFlask, EcosystemFastAPI, EcosystemDjango, EcosystemSQLAlchemy, EcosystemPyTorch, EcosystemOpenCV, EcosystemUltralytics, EcosystemASPNetCore, EcosystemEFCore, EcosystemTokio, EcosystemAxum, EcosystemActix, EcosystemSQLx, EcosystemDiesel, EcosystemReqwest, EcosystemTonic} {
		if !index.HasEcosystem(ecosystem) {
			t.Errorf("missing ecosystem %s", ecosystem)
		}
	}
}

func itoa(value int) string {
	if value < 10 {
		return string(rune('0' + value))
	}
	return ""
}
