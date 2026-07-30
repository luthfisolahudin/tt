package worker

import "fmt"

type Tier struct {
	Name   string
	Model  string
	Effort string
}

var TierRegistry = map[string]Tier{
	"default": {
		Name:   "default",
		Model:  "cliproxy/gemini-3.6-flash-high",
		Effort: "high",
	},
}

const TierDefault = "default"

func GetTier(name string) (Tier, error) {
	tier, ok := TierRegistry[name]
	if !ok {
		return Tier{}, fmt.Errorf("unknown tier: %s", name)
	}
	return tier, nil
}

func IsKnownTier(name string) bool {
	_, ok := TierRegistry[name]
	return ok
}

func TierNames() []string {
	names := make([]string, 0, len(TierRegistry))
	for name := range TierRegistry {
		names = append(names, name)
	}
	return names
}
