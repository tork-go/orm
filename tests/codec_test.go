package orm_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tork-go/orm"
)

type prefs struct {
	Theme string `json:"theme"`
}

// Query building reaches columns as ColumnMeta and asserts to ValueCodec
// when it has to encode or decode a value. A column type failing that
// assertion would be silently unusable for generated values and document
// columns alike, so the assertion is worth making from the outside.
func TestValueCodec_SatisfiedThroughColumnMeta(t *testing.T) {
	cols := []orm.ColumnMeta{
		orm.NewIntColumn("i"),
		orm.NewStringColumn("s"),
		orm.NewNullableStringColumn("ns"),
		orm.NewJSONColumn[prefs]("j"),
		orm.NewNullableJSONColumn[prefs]("nj"),
		orm.NewEnumColumn("e", "t", "a"),
		orm.NewStringArrayColumn("a"),
		orm.NewUUIDColumn("u"),
		orm.NewColumn[int]("plain"),
	}
	for _, c := range cols {
		if _, ok := c.(orm.ValueCodec); !ok {
			t.Errorf("column %q does not satisfy ValueCodec", c.Name())
		}
	}
}

func TestGenerateAny(t *testing.T) {
	plain := orm.NewUUIDColumn("id")
	if _, ok := any(plain).(orm.ValueCodec).GenerateAny(); ok {
		t.Error("GenerateAny() reported a generator on a column that has none")
	}

	generated := orm.NewUUIDColumn("id").GeneratedByClient(uuid.New)
	v, ok := any(generated).(orm.ValueCodec).GenerateAny()
	if !ok {
		t.Fatal("GenerateAny() = false, want a generated value")
	}
	id, ok := v.(uuid.UUID)
	if !ok {
		t.Fatalf("GenerateAny() returned %T, want uuid.UUID", v)
	}
	if id == uuid.Nil {
		t.Error("GenerateAny() produced the nil UUID")
	}
}

// A JSON column with no Serialize call still has to encode somehow. Before
// ValueCodec nothing defined what that meant, so the default is asserted
// rather than assumed.
func TestMarshalAny_DefaultsToJSON(t *testing.T) {
	c := any(orm.NewJSONColumn[prefs]("p")).(orm.ValueCodec)

	b, err := c.MarshalAny(prefs{Theme: "dark"})
	if err != nil {
		t.Fatalf("MarshalAny() error = %v", err)
	}
	if got, want := string(b), `{"theme":"dark"}`; got != want {
		t.Errorf("MarshalAny() = %s, want %s", got, want)
	}

	v, err := c.UnmarshalAny([]byte(`{"theme":"light"}`))
	if err != nil {
		t.Fatalf("UnmarshalAny() error = %v", err)
	}
	got, ok := v.(prefs)
	if !ok {
		t.Fatalf("UnmarshalAny() returned %T, want prefs", v)
	}
	if got.Theme != "light" {
		t.Errorf("UnmarshalAny() theme = %q, want %q", got.Theme, "light")
	}
}

func TestSerialize_OverridesTheDefault(t *testing.T) {
	marshal := func(p prefs) ([]byte, error) { return []byte("custom:" + p.Theme), nil }
	unmarshal := func(b []byte) (prefs, error) {
		return prefs{Theme: strings.TrimPrefix(string(b), "custom:")}, nil
	}

	c := any(orm.NewJSONColumn[prefs]("p").Serialize(marshal, unmarshal)).(orm.ValueCodec)

	b, err := c.MarshalAny(prefs{Theme: "dark"})
	if err != nil {
		t.Fatalf("MarshalAny() error = %v", err)
	}
	if got := string(b); got != "custom:dark" {
		t.Errorf("MarshalAny() = %s, want custom:dark", got)
	}

	v, err := c.UnmarshalAny([]byte("custom:light"))
	if err != nil {
		t.Fatalf("UnmarshalAny() error = %v", err)
	}
	if got := v.(prefs).Theme; got != "light" {
		t.Errorf("UnmarshalAny() theme = %q, want %q", got, "light")
	}
}

// The whole point of the type erasure is that a caller holding a
// ColumnMeta cannot know T, so handing over the wrong type has to be
// reported rather than allowed to corrupt the row.
func TestMarshalAny_WrongType(t *testing.T) {
	c := any(orm.NewJSONColumn[prefs]("p")).(orm.ValueCodec)

	_, err := c.MarshalAny("not prefs")
	if err == nil {
		t.Fatal("MarshalAny() error = nil, want a type mismatch")
	}
	for _, want := range []string{`column "p"`, "string", "prefs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestUnmarshalAny_ErrorsAreNamed(t *testing.T) {
	c := any(orm.NewJSONColumn[prefs]("p")).(orm.ValueCodec)
	_, err := c.UnmarshalAny([]byte("not json"))
	if err == nil {
		t.Fatal("UnmarshalAny() error = nil, want a decode failure")
	}
	if !strings.Contains(err.Error(), `column "p"`) {
		t.Errorf("error %q does not name the column", err)
	}
}

func TestUnmarshalAny_CustomErrorIsWrapped(t *testing.T) {
	sentinel := errors.New("boom")
	c := any(orm.NewJSONColumn[prefs]("p").Serialize(
		func(prefs) ([]byte, error) { return nil, nil },
		func([]byte) (prefs, error) { return prefs{}, sentinel },
	)).(orm.ValueCodec)

	_, err := c.UnmarshalAny(nil)
	if !errors.Is(err, sentinel) {
		t.Errorf("UnmarshalAny() error = %v, want it to wrap the codec's own error", err)
	}
}
