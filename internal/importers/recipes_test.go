package importers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"GoMental/internal/okf"
)

func TestRecipeImporterImportsSchemaRecipeJSONLD(t *testing.T) {
	source := ImportSource{
		URL:         "https://example.com/original",
		ContentType: "text/html",
		HTML: []byte(`<!doctype html>
<html>
<head>
  <title>Ignored title</title>
  <link rel="canonical" href="https://example.com/ramen">
  <script type="application/ld+json">
  {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "Recipe",
        "name": "Weeknight Ramen",
        "description": "Fast bowl for tired evenings.",
        "author": {"@type": "Person", "name": "Ada"},
        "prepTime": "PT10M",
        "cookTime": "PT20M",
        "totalTime": "PT30M",
        "recipeYield": "2 bowls",
        "recipeCategory": "Dinner",
        "recipeCuisine": "Japanese",
        "keywords": "ramen, noodles",
        "recipeIngredient": ["2 eggs", "200g noodles"],
        "recipeInstructions": [
          {"@type": "HowToStep", "text": "Boil the noodles."},
          {"@type": "HowToSection", "itemListElement": [
            {"@type": "HowToStep", "name": "Finish", "text": "Top with eggs."}
          ]}
        ],
        "aggregateRating": {"ratingValue": "4.8", "reviewCount": "128"}
      }
    ]
  }
  </script>
</head>
</html>`),
	}
	importer := RecipeImporter{BasePath: "recipes", Now: func() time.Time {
		return time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	}}

	match, err := importer.CanImport(context.Background(), source)
	if err != nil {
		t.Fatalf("can import: %v", err)
	}
	if match.ImporterID != RecipeImporterID || match.NoteType != "recipe" || match.Confidence < 0.9 {
		t.Fatalf("unexpected match: %#v", match)
	}

	result, err := importer.Import(context.Background(), source)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Document.ID != "recipes/weeknight-ramen" {
		t.Fatalf("unexpected note ID: %q", result.Document.ID)
	}
	raw := result.Document.Document.Raw
	for _, want := range []string{
		"type: recipe",
		"title: Weeknight Ramen",
		"resource: https://example.com/ramen",
		"importer: schema.org.recipe",
		"source_url: https://example.com/ramen",
		"# Weeknight Ramen",
		"## Ingredients",
		"- 2 eggs",
		"1. Boil the noodles.",
		"1. Top with eggs.",
		"**Total time:** PT30M",
		"**Rating:** 4.8 (128 reviews)",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("expected imported note to contain %q:\n%s", want, raw)
		}
	}

	parsed, err := okf.NewCodec().Decode(result.Document.ID, raw, time.Now())
	if err != nil {
		t.Fatalf("decode imported OKF: %v", err)
	}
	if parsed.Metadata.Type != "recipe" || parsed.Title != "Weeknight Ramen" {
		t.Fatalf("unexpected parsed metadata: %#v", parsed.Metadata)
	}
	if len(parsed.Tags) == 0 {
		t.Fatal("expected recipe tags")
	}
}

func TestRegistryReturnsNoImporterForNonRecipe(t *testing.T) {
	registry := DefaultRegistry()
	_, err := registry.Import(context.Background(), ImportSource{
		URL:         "https://example.com/article",
		ContentType: "text/html",
		HTML:        []byte(`<html><head><title>Article</title></head></html>`),
	})
	if !errors.Is(err, ErrNoImporter) {
		t.Fatalf("expected ErrNoImporter, got %v", err)
	}
}
