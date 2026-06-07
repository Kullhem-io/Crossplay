package schema

// DeltaType enumerates the mechanical changes the DM may apply to the ledger.
// The set is deliberately small; novel *content* (new status names, new items)
// is unbounded, but every change funnels through one of these verbs so the
// engine can keep the books balanced.
type DeltaType string

const (
	DeltaDamage     DeltaType = "damage"      // target loses HP
	DeltaHeal       DeltaType = "heal"        // target gains HP (if alive)
	DeltaStatus     DeltaType = "status"      // add (or remove) a status effect
	DeltaItemAdd    DeltaType = "item_add"    // grant an item to a target
	DeltaItemRemove DeltaType = "item_remove" // consume/drop an item
	DeltaXP         DeltaType = "xp"          // award XP (engine handles level-up)
)

// Delta is one proposed change. Target is an entity id (preferred) or name.
type Delta struct {
	Type   DeltaType `json:"type"`
	Target string    `json:"target"`
	Amount int       `json:"amount,omitempty"` // for damage/heal
	Status string    `json:"status,omitempty"` // for status
	Remove bool      `json:"remove,omitempty"` // status: remove instead of add
	Item   string    `json:"item,omitempty"`   // for item_*
	Qty    int       `json:"qty,omitempty"`    // for item_* (default 1)
	Note   string    `json:"note,omitempty"`   // optional rationale
}

// Adjudication is the DM's structured ruling for one action. The engine has
// already rolled the dice; the DM interprets the roll into concrete effects.
type Adjudication struct {
	Outcome   string  `json:"outcome"`   // one factual sentence: what happened
	Deltas    []Delta `json:"deltas"`    // mechanical changes to apply
	Narration string  `json:"narration"` // a hint for the narrator to dramatize
}

// AdjudicationSchema constrains the DM's output (flat, grammar-friendly).
var AdjudicationSchema = []byte(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "outcome": { "type": "string" },
    "narration": { "type": "string" },
    "deltas": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "type": { "type": "string", "enum": ["damage", "heal", "status", "item_add", "item_remove", "xp"] },
          "target": { "type": "string" },
          "amount": { "type": "integer" },
          "status": { "type": "string" },
          "remove": { "type": "boolean" },
          "item": { "type": "string" },
          "qty": { "type": "integer" },
          "note": { "type": "string" }
        },
        "required": ["type", "target"]
      }
    }
  },
  "required": ["outcome", "deltas", "narration"]
}`)
