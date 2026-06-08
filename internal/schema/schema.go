// Package schema defines the authoritative game ledger types. These are the
// single source of truth shared with the front end via Go->TS generation
// (see tygo.yaml). The engine owns and mutates GameState; models only propose
// changes to it.
package schema

// Phase is the lifecycle of a game.
type Phase string

const (
	PhaseLobby    Phase = "lobby"
	PhasePlaying  Phase = "playing"
	PhaseGameOver Phase = "game_over"
)

// EntityKind distinguishes who decides an entity's actions.
type EntityKind string

const (
	KindPlayer  EntityKind = "player"
	KindMonster EntityKind = "monster"
	KindNPC     EntityKind = "npc"
)

// GameState is the canonical ledger. Hard facts only; narrative "soft" world
// detail lives in the models' context, not here.
type GameState struct {
	Topic    string   `json:"topic"`
	Phase    Phase    `json:"phase"`
	Outcome  Outcome  `json:"outcome"` // set when Phase is game_over
	Round    int      `json:"round"`
	Location Location `json:"location"`
	Entities []Entity `json:"entities"`
	Log      []string `json:"log"`
}

// Outcome records how a finished game ended.
type Outcome string

const (
	OutcomeNone    Outcome = ""
	OutcomeVictory Outcome = "victory"
	OutcomeDefeat  Outcome = "defeat"
)

// Location is the current scene.
type Location struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Entity is any actor on the ledger, the player and monsters share this type;
// only Kind (and thus who decides their actions) differs.
type Entity struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Kind      EntityKind `json:"kind"`
	Class     string     `json:"class"` // archetype, e.g. "Rogue", "Brute"
	Level     int        `json:"level"`
	XP        int        `json:"xp"`
	HP        int        `json:"hp"`
	MaxHP     int        `json:"maxHp"`
	Alive     bool       `json:"alive"`
	Status    []string   `json:"status"`
	Inventory []Item     `json:"inventory"`
	Desc      string     `json:"desc"`
}

// XPForNext is the XP needed to advance from the entity's current level.
func (e *Entity) XPForNext() int {
	if e.Level < 1 {
		return 10
	}
	return e.Level * 10
}

// Item is a stackable inventory entry.
type Item struct {
	Name string `json:"name"`
	Qty  int    `json:"qty"`
}

// Player returns the first player entity, or nil if none.
func (g *GameState) Player() *Entity {
	for i := range g.Entities {
		if g.Entities[i].Kind == KindPlayer {
			return &g.Entities[i]
		}
	}
	return nil
}

// Players returns pointers to all player entities (the party).
func (g *GameState) Players() []*Entity {
	var out []*Entity
	for i := range g.Entities {
		if g.Entities[i].Kind == KindPlayer {
			out = append(out, &g.Entities[i])
		}
	}
	return out
}

// LivingPlayers returns pointers to alive player entities.
func (g *GameState) LivingPlayers() []*Entity {
	var out []*Entity
	for i := range g.Entities {
		if g.Entities[i].Kind == KindPlayer && g.Entities[i].Alive {
			out = append(out, &g.Entities[i])
		}
	}
	return out
}

// LivingMonsters returns pointers to alive monster entities.
func (g *GameState) LivingMonsters() []*Entity {
	var out []*Entity
	for i := range g.Entities {
		if g.Entities[i].Kind == KindMonster && g.Entities[i].Alive {
			out = append(out, &g.Entities[i])
		}
	}
	return out
}

// FindEntity returns the entity with the given id, or nil.
func (g *GameState) FindEntity(id string) *Entity {
	for i := range g.Entities {
		if g.Entities[i].ID == id {
			return &g.Entities[i]
		}
	}
	return nil
}
