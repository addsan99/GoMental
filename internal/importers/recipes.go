package importers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"GoMental/internal/domain"
	"GoMental/internal/okf"
)

const RecipeImporterID = "schema.org.recipe"

type SchemaRecipe struct {
	Name        string
	URL         string
	ImageURLs   []string
	Author      []string
	Description string

	PrepTime  string
	CookTime  string
	TotalTime string

	RecipeYield      []string
	RecipeCategory   []string
	RecipeCuisine    []string
	Keywords         []string
	RecipeIngredient []string
	Instructions     []string

	RatingValue string
	RatingCount string
	ReviewCount string
}

type RecipeImporter struct {
	BasePath string
	Now      func() time.Time
}

func NewRecipeImporter() RecipeImporter {
	return RecipeImporter{BasePath: "recipes"}
}

func (r RecipeImporter) ID() string {
	return RecipeImporterID
}

func (r RecipeImporter) CanImport(ctx context.Context, source ImportSource) (ImportMatch, error) {
	if err := ctx.Err(); err != nil {
		return ImportMatch{}, err
	}
	page, err := source.ExtractedPage()
	if err != nil {
		return ImportMatch{}, err
	}
	recipe, ok := firstRecipe(page)
	if !ok {
		return ImportMatch{}, nil
	}
	confidence := 0.75
	if recipe.Name != "" {
		confidence += 0.1
	}
	if len(recipe.RecipeIngredient) > 0 {
		confidence += 0.1
	}
	if len(recipe.Instructions) > 0 {
		confidence += 0.05
	}
	if confidence > 1 {
		confidence = 1
	}
	return ImportMatch{ImporterID: r.ID(), NoteType: "recipe", Confidence: confidence}, nil
}

func (r RecipeImporter) Import(ctx context.Context, source ImportSource) (*ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	page, err := source.ExtractedPage()
	if err != nil {
		return nil, err
	}
	recipe, ok := firstRecipe(page)
	if !ok {
		return nil, ErrNoImporter
	}
	title := firstNonEmpty(recipe.Name, page.Title, "Imported recipe")
	sourceURL := firstNonEmpty(recipe.URL, page.CanonicalURL, source.URL)
	timestamp := time.Now().UTC()
	if r.Now != nil {
		timestamp = r.Now().UTC()
	}
	body := renderRecipeBody(recipe, title, sourceURL)
	document, err := okf.NewCodec().Encode(domain.OKFMetadata{
		Type:        "recipe",
		Title:       title,
		Description: recipe.Description,
		Resource:    sourceURL,
		Tags:        recipeTags(recipe),
		Timestamp:   &timestamp,
		Unknown: map[string]any{
			"importer":    r.ID(),
			"source_url":  sourceURL,
			"schema_type": "Recipe",
		},
	}, body)
	if err != nil {
		return nil, err
	}
	return &ImportResult{
		Document: domain.Note{
			ID:       r.noteID(title, sourceURL),
			Document: document,
		},
		Confidence: 0.95,
	}, nil
}

func firstRecipe(page ExtractedPage) (SchemaRecipe, bool) {
	for _, raw := range page.JSONLD {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		var objects []map[string]any
		findTypedObjects(value, "Recipe", &objects)
		for _, object := range objects {
			recipe := schemaRecipeFromMap(object)
			if recipe.Name != "" || len(recipe.RecipeIngredient) > 0 || len(recipe.Instructions) > 0 {
				return recipe, true
			}
		}
	}
	return SchemaRecipe{}, false
}

func findTypedObjects(value any, wantedType string, results *[]map[string]any) {
	switch v := value.(type) {
	case map[string]any:
		if hasSchemaType(v["@type"], wantedType) {
			*results = append(*results, v)
		}
		for _, child := range v {
			findTypedObjects(child, wantedType, results)
		}
	case []any:
		for _, child := range v {
			findTypedObjects(child, wantedType, results)
		}
	}
}

func hasSchemaType(value any, wanted string) bool {
	switch t := value.(type) {
	case string:
		return strings.EqualFold(strings.TrimPrefix(t, "schema:"), wanted)
	case []any:
		for _, item := range t {
			if hasSchemaType(item, wanted) {
				return true
			}
		}
	case []string:
		for _, item := range t {
			if hasSchemaType(item, wanted) {
				return true
			}
		}
	}
	return false
}

func schemaRecipeFromMap(value map[string]any) SchemaRecipe {
	recipe := SchemaRecipe{
		Name:             scalar(value["name"]),
		URL:              scalar(value["url"]),
		ImageURLs:        textList(value["image"]),
		Author:           peopleList(value["author"]),
		Description:      scalar(value["description"]),
		PrepTime:         scalar(value["prepTime"]),
		CookTime:         scalar(value["cookTime"]),
		TotalTime:        scalar(value["totalTime"]),
		RecipeYield:      textList(value["recipeYield"]),
		RecipeCategory:   textList(value["recipeCategory"]),
		RecipeCuisine:    textList(value["recipeCuisine"]),
		Keywords:         keywordList(value["keywords"]),
		RecipeIngredient: textList(value["recipeIngredient"]),
		Instructions:     instructionList(value["recipeInstructions"]),
	}
	if rating, ok := value["aggregateRating"].(map[string]any); ok {
		recipe.RatingValue = scalar(rating["ratingValue"])
		recipe.RatingCount = scalar(rating["ratingCount"])
		recipe.ReviewCount = scalar(rating["reviewCount"])
	}
	return recipe
}

func renderRecipeBody(recipe SchemaRecipe, title string, sourceURL string) string {
	var buf bytes.Buffer
	buf.WriteString("# ")
	buf.WriteString(title)
	buf.WriteString("\n\n")
	if recipe.Description != "" {
		buf.WriteString(recipe.Description)
		buf.WriteString("\n\n")
	}
	if sourceURL != "" {
		buf.WriteString("Source: [")
		buf.WriteString(markdownEscape(displayURL(sourceURL)))
		buf.WriteString("](")
		buf.WriteString(sourceURL)
		buf.WriteString(")\n\n")
	}
	writeDetails(&buf, recipe)
	writeListSection(&buf, "Ingredients", recipe.RecipeIngredient, "- ")
	writeListSection(&buf, "Instructions", recipe.Instructions, "1. ")
	return strings.TrimRight(buf.String(), "\n") + "\n"
}

func writeDetails(buf *bytes.Buffer, recipe SchemaRecipe) {
	rows := [][2]string{
		{"Prep time", recipe.PrepTime},
		{"Cook time", recipe.CookTime},
		{"Total time", recipe.TotalTime},
		{"Yield", strings.Join(recipe.RecipeYield, ", ")},
		{"Category", strings.Join(recipe.RecipeCategory, ", ")},
		{"Cuisine", strings.Join(recipe.RecipeCuisine, ", ")},
		{"Author", strings.Join(recipe.Author, ", ")},
		{"Rating", ratingText(recipe)},
	}
	wroteHeader := false
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		if !wroteHeader {
			buf.WriteString("## Details\n\n")
			wroteHeader = true
		}
		buf.WriteString("- **")
		buf.WriteString(row[0])
		buf.WriteString(":** ")
		buf.WriteString(row[1])
		buf.WriteString("\n")
	}
	if wroteHeader {
		buf.WriteString("\n")
	}
}

func writeListSection(buf *bytes.Buffer, heading string, values []string, prefix string) {
	if len(values) == 0 {
		return
	}
	buf.WriteString("## ")
	buf.WriteString(heading)
	buf.WriteString("\n\n")
	for _, value := range values {
		buf.WriteString(prefix)
		buf.WriteString(value)
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
}

func (r RecipeImporter) noteID(title string, sourceURL string) domain.NoteID {
	basePath := strings.Trim(strings.ReplaceAll(r.BasePath, `\`, "/"), "/")
	if basePath == "" {
		basePath = "recipes"
	}
	noteSlug := slug(title)
	if noteSlug == "" {
		noteSlug = slug(hostFromURL(sourceURL))
	}
	if noteSlug == "" {
		noteSlug = "imported-recipe"
	}
	return domain.NoteID(basePath + "/" + noteSlug)
}

func recipeTags(recipe SchemaRecipe) []domain.Tag {
	seen := map[string]struct{}{"recipe": {}}
	tags := []domain.Tag{"recipe"}
	for _, value := range append(append([]string{}, recipe.RecipeCategory...), recipe.RecipeCuisine...) {
		tag := slug(value)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, domain.Tag(tag))
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })
	return tags
}

func instructionList(value any) []string {
	var out []string
	var walk func(any)
	walk = func(item any) {
		switch v := item.(type) {
		case nil:
			return
		case string:
			addText(&out, v)
		case []any:
			for _, child := range v {
				walk(child)
			}
		case []string:
			for _, child := range v {
				addText(&out, child)
			}
		case map[string]any:
			if hasSchemaType(v["@type"], "HowToSection") {
				walk(v["itemListElement"])
				return
			}
			addText(&out, firstNonEmpty(scalar(v["text"]), scalar(v["name"])))
			walk(v["itemListElement"])
		}
	}
	walk(value)
	return out
}

func peopleList(value any) []string {
	var out []string
	var walk func(any)
	walk = func(item any) {
		switch v := item.(type) {
		case string:
			addText(&out, v)
		case []any:
			for _, child := range v {
				walk(child)
			}
		case map[string]any:
			addText(&out, firstNonEmpty(scalar(v["name"]), scalar(v["@id"])))
		}
	}
	walk(value)
	return out
}

func textList(value any) []string {
	var out []string
	var walk func(any)
	walk = func(item any) {
		switch v := item.(type) {
		case nil:
			return
		case string:
			addText(&out, v)
		case []string:
			for _, child := range v {
				addText(&out, child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		case map[string]any:
			addText(&out, firstNonEmpty(scalar(v["name"]), scalar(v["url"]), scalar(v["@id"])))
		default:
			addText(&out, fmt.Sprint(v))
		}
	}
	walk(value)
	return out
}

func keywordList(value any) []string {
	if text, ok := value.(string); ok {
		var out []string
		for _, part := range strings.Split(text, ",") {
			addText(&out, part)
		}
		return out
	}
	return textList(value)
}

func addText(values *[]string, raw string) {
	text := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if text != "" {
		*values = append(*values, text)
	}
}

func scalar(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func ratingText(recipe SchemaRecipe) string {
	if recipe.RatingValue == "" {
		return ""
	}
	count := firstNonEmpty(recipe.RatingCount, recipe.ReviewCount)
	if count == "" {
		return recipe.RatingValue
	}
	return recipe.RatingValue + " (" + count + " reviews)"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func displayURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return raw
	}
	return parsed.Host
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}

func slug(raw string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func markdownEscape(value string) string {
	replacer := strings.NewReplacer("[", `\[`, "]", `\]`)
	return replacer.Replace(value)
}

func IsNoImporter(err error) bool {
	return errors.Is(err, ErrNoImporter)
}
