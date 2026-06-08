package schema

// WorldgenResult is what the Qwen worldgen call returns (grammar-constrained
// by WorldgenSchema). The engine validates it and builds the canonical
// GameState from it, models never construct the ledger directly.
type WorldgenResult struct {
	Location Location          `json:"location"`
	Players  []WorldgenPlayer  `json:"players"`
	Monsters []WorldgenMonster `json:"monsters"`
}

type WorldgenPlayer struct {
	Name      string `json:"name"`
	Class     string `json:"class"`
	Desc      string `json:"desc"`
	MaxHP     int    `json:"maxHp"`
	Inventory []Item `json:"inventory"`
}

type WorldgenMonster struct {
	Name  string `json:"name"`
	Desc  string `json:"desc"`
	MaxHP int    `json:"maxHp"`
}

// WorldgenSchema is a compact, llama.cpp-grammar-friendly JSON schema matching
// WorldgenResult. Hand-written (flat, no $ref) for reliable grammar conversion.
var WorldgenSchema = []byte(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "location": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "name": { "type": "string" },
        "description": { "type": "string" }
      },
      "required": ["name", "description"]
    },
    "players": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "name": { "type": "string" },
          "class": { "type": "string" },
          "desc": { "type": "string" },
          "maxHp": { "type": "integer", "minimum": 1 },
          "inventory": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "properties": {
                "name": { "type": "string" },
                "qty": { "type": "integer", "minimum": 1 }
              },
              "required": ["name", "qty"]
            }
          }
        },
        "required": ["name", "class", "desc", "maxHp", "inventory"]
      }
    },
    "monsters": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "name": { "type": "string" },
          "desc": { "type": "string" },
          "maxHp": { "type": "integer", "minimum": 1 }
        },
        "required": ["name", "desc", "maxHp"]
      }
    }
  },
  "required": ["location", "players", "monsters"]
}`)
