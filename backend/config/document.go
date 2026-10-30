package config

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	maxDocumentBytes  = 1 << 20
	maxNameLength     = 255
	maxCheckerPath    = 1024
	maxCheckerActions = 1024
	maxCheckerType    = 32
	maxLogoLength     = 255
)

type Document struct {
	Admin    *Credentials `yaml:"admin,omitempty"`
	Game     Game         `yaml:"game"`
	Tasks    []Task       `yaml:"tasks"`
	Teams    []Team       `yaml:"teams"`
	Storages *Storages    `yaml:"storages,omitempty"`
}

type Credentials struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}
type Storages struct {
	DB       Database `yaml:"db"`
	RabbitMQ Rabbit   `yaml:"rabbitmq"`
	Redis    Redis    `yaml:"redis"`
}
type Database struct {
	User     string       `yaml:"user"`
	Password string       `yaml:"password"`
	Host     string       `yaml:"host"`
	Port     int          `yaml:"port"`
	DBName   string       `yaml:"dbname"`
	Runtime  *Credentials `yaml:"runtime,omitempty"`
}
type Rabbit struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	VHost    string `yaml:"vhost"`
}
type Redis struct {
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	DB       int    `yaml:"db"`
}

type Game struct {
	Mode             string  `yaml:"mode"`
	RoundTime        int     `yaml:"round_time"`
	ExtraChecks      int     `yaml:"extra_checks"`
	Rounds           *int    `yaml:"rounds"`
	StartTime        string  `yaml:"start_time"`
	Timezone         string  `yaml:"timezone"`
	DefaultScore     int     `yaml:"default_score"`
	FlagLifetime     int     `yaml:"flag_lifetime"`
	GameHardness     float64 `yaml:"game_hardness"`
	Inflation        bool    `yaml:"inflation"`
	VolgaAttacksMode bool    `yaml:"volga_attacks_mode"`
	CheckersPath     string  `yaml:"checkers_path"`
	EnvPath          string  `yaml:"env_path"`
	GetPeriod        *int    `yaml:"get_period"`
}

type Task struct {
	Name           string  `yaml:"name"`
	Checker        string  `yaml:"checker"`
	CheckerType    string  `yaml:"checker_type"`
	CheckerTimeout *int    `yaml:"checker_timeout"`
	Puts           *int    `yaml:"puts"`
	Gets           *int    `yaml:"gets"`
	Places         *int    `yaml:"places"`
	EnvPath        *string `yaml:"env_path"`
	DefaultScore   *int    `yaml:"default_score"`
	GetPeriod      *int    `yaml:"get_period"`
}

type Team struct {
	IP           string   `yaml:"ip"`
	Name         string   `yaml:"name"`
	Highlighted  bool     `yaml:"highlighted"`
	LogoPath     string   `yaml:"logo_path,omitempty"`
	Token        string   `yaml:"token,omitempty"`
	TrafficCIDRs []string `yaml:"traffic_cidrs,omitempty"`
}

func LoadDocument(filename string) (Document, error) {
	const location = "config.LoadDocument"
	f, err := os.Open(filename)
	if err != nil {
		return Document{}, fmt.Errorf("%s - open configuration file: %w", location, err)
	}
	defer func() { _ = f.Close() }()
	return DecodeDocument(f)
}

func DecodeDocument(reader io.Reader) (Document, error) {
	const location = "config.DecodeDocument"
	d := Document{Game: Game{Mode: "classic", Timezone: "UTC", DefaultScore: 2500, GameHardness: 10, Inflation: true, CheckersPath: "/checkers/", EnvPath: "/opt/checkers/bin"}}
	data, err := io.ReadAll(io.LimitReader(reader, maxDocumentBytes+1))
	if err != nil {
		return Document{}, fmt.Errorf("%s - read configuration: %w", location, err)
	}
	if len(data) > maxDocumentBytes {
		return Document{}, fmt.Errorf("%s - configuration exceeds 1 MiB", location)
	}
	var tree yaml.Node
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return Document{}, fmt.Errorf("%s - invalid YAML configuration", location)
	}
	if err := validateScalarTypes(&tree); err != nil {
		return Document{}, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&d); err != nil {
		return Document{}, fmt.Errorf("%s - invalid YAML configuration or field type", location)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Document{}, fmt.Errorf("%s - configuration must contain exactly one YAML document", location)
	}
	for i := range d.Tasks {
		t := &d.Tasks[i]
		if strings.TrimSpace(t.Checker) == "" {
			return Document{}, fmt.Errorf("%s - tasks[%d] requires a checker path", location, i)
		}
		t.applyDefaults(d.Game)
	}
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	return d, nil
}

func (t *Task) applyDefaults(game Game) {
	if t.CheckerType == "" {
		t.CheckerType = "hackerdom"
	}
	t.CheckerTimeout = defaultPointer(t.CheckerTimeout, 10)
	t.Puts = defaultPointer(t.Puts, 1)
	t.Gets = defaultPointer(t.Gets, 1)
	t.Places = defaultPointer(t.Places, 1)
	t.DefaultScore = defaultPointer(t.DefaultScore, game.DefaultScore)
	t.EnvPath = defaultPointer(t.EnvPath, game.EnvPath)
	getPeriod := game.RoundTime
	if game.GetPeriod != nil {
		getPeriod = *game.GetPeriod
	}
	t.GetPeriod = defaultPointer(t.GetPeriod, getPeriod)
	if !path.IsAbs(t.Checker) {
		t.Checker = path.Join(game.CheckersPath, t.Checker)
	}
}

func defaultPointer[T any](value *T, fallback T) *T {
	if value == nil {
		return new(fallback)
	}
	return value
}

// yaml.v3 accepts a floating-point scalar when decoding an int. Reject that
// coercion for round counts and limits, where silently truncating is unsafe.
func validateScalarTypes(node *yaml.Node) error {
	const location = "config.validateScalarTypes"
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("%s - YAML aliases are not supported in game configuration", location)
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i].Value, node.Content[i+1]
			switch key {
			case "round_time", "rounds", "extra_checks", "flag_lifetime", "puts", "gets", "places", "checker_timeout", "get_period", "port", "db":
				// storages.db is a mapping; nullable values retain their defaults.
				if value.Kind == yaml.ScalarNode && value.Tag != "!!int" && value.Tag != "!!null" {
					return fmt.Errorf("%s - %s must be an integer", location, key)
				}
			case "token":
				if value.Tag == "!!null" {
					continue
				}
				if value.Tag != "!!str" || !tokenPattern.MatchString(value.Value) {
					return fmt.Errorf("%s - team token must be a string of 16 lowercase hex characters", location)
				}
			}
		}
	}
	for _, child := range node.Content {
		if err := validateScalarTypes(child); err != nil {
			return err
		}
	}
	return nil
}

func (g Game) StartAt() (time.Time, error) {
	const location = "config.Game.StartAt"
	timezoneLocation, err := time.LoadLocation(g.Timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s - invalid game timezone", location)
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		at, err := time.ParseInLocation(layout, g.StartTime, timezoneLocation)
		if err == nil {
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("%s - game.start_time must be a date and time", location)
}

var tokenPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (d Document) Validate() error {
	const location = "config.Document.Validate"
	if err := d.Game.validate(); err != nil {
		return err
	}
	if len(d.Teams) == 0 || len(d.Tasks) == 0 {
		return fmt.Errorf("%s - configure at least one team and one task", location)
	}
	if err := validateTeams(d.Teams); err != nil {
		return err
	}
	for index, task := range d.Tasks {
		if err := task.validate(index); err != nil {
			return err
		}
		if d.Game.ExtraChecks > 0 && *task.CheckerTimeout+3 >= d.Game.RoundTime {
			return fmt.Errorf("%s - extra checks require round_time > checker_timeout + 3 for tasks[%d]", location, index)
		}
	}
	return nil
}

func (g Game) validate() error {
	const location = "config.Game.validate"
	if g.Mode != "classic" && g.Mode != "blitz" {
		return fmt.Errorf("%s - game.mode must be classic or blitz", location)
	}
	if g.ExtraChecks < 0 || g.ExtraChecks > 8 || (g.ExtraChecks > 0 && g.Mode != "classic") {
		return fmt.Errorf("%s - game.extra_checks must be 0..8 and requires classic mode when enabled", location)
	}
	if err := g.validateRounds(); err != nil {
		return err
	}
	if math.IsNaN(g.GameHardness) || math.IsInf(g.GameHardness, 0) || g.GameHardness <= 1 {
		return fmt.Errorf("%s - game.game_hardness must be finite and greater than 1", location)
	}
	if _, err := g.StartAt(); err != nil {
		return err
	}
	if g.DefaultScore < 0 || g.DefaultScore > math.MaxInt32 {
		return fmt.Errorf("%s - game.default_score must be a nonnegative 32-bit integer", location)
	}
	return nil
}

func (g Game) validateRounds() error {
	const location = "config.Game.validateRounds"
	if g.RoundTime < 1 || g.FlagLifetime < 1 || g.RoundTime > math.MaxInt32 || g.FlagLifetime > math.MaxInt32 {
		return fmt.Errorf("%s - round_time and flag_lifetime must be positive 32-bit integers", location)
	}
	if g.Rounds != nil && (*g.Rounds < 1 || *g.Rounds > math.MaxInt32) {
		return fmt.Errorf("%s - game.rounds must be a positive 32-bit integer or null", location)
	}
	return nil
}

func validateTeams(teams []Team) error {
	const location = "config.validateTeams"
	tokens := make(map[string]bool, len(teams))
	var networks []netip.Prefix
	var owners []int
	for i, t := range teams {
		if strings.TrimSpace(t.Name) == "" || len([]rune(t.Name)) > maxNameLength {
			return fmt.Errorf("%s - teams[%d].name is invalid", location, i)
		}
		if _, err := netip.ParseAddr(t.IP); err != nil {
			return fmt.Errorf("%s - teams[%d].ip must be an IPv4 or IPv6 address", location, i)
		}
		prefixes, err := t.TrafficNetworks()
		if err != nil {
			return fmt.Errorf("%s - teams[%d] traffic networks: %w", location, i, err)
		}
		for _, prefix := range prefixes {
			for j, previous := range networks {
				if prefix.Overlaps(previous) {
					return fmt.Errorf("%s - teams[%d] traffic network overlaps teams[%d]", location, i, owners[j])
				}
			}
			networks = append(networks, prefix)
			owners = append(owners, i)
		}
		if t.Token != "" {
			if !tokenPattern.MatchString(t.Token) || tokens[t.Token] {
				return fmt.Errorf("%s - teams[%d].token must be unique and contain 16 lowercase hex characters", location, i)
			}
			tokens[t.Token] = true
		}
		if !ValidLogoPath(t.LogoPath) {
			return fmt.Errorf("%s - teams[%d].logo_path must be a safe path or HTTP(S) URL", location, i)
		}
	}
	return nil
}

func (t Task) validate(index int) error {
	const location = "config.Task.validate"
	if strings.TrimSpace(t.Name) == "" || len([]rune(t.Name)) > maxNameLength || t.Checker == "" || len(t.Checker) > maxCheckerPath {
		return fmt.Errorf("%s - tasks[%d] requires a name and checker path", location, index)
	}
	if err := t.validateActionLimits(index); err != nil {
		return err
	}
	if t.EnvPath == nil || len(*t.EnvPath) > maxCheckerPath || len(t.CheckerType) > maxCheckerType {
		return fmt.Errorf("%s - tasks[%d] checker settings are invalid", location, index)
	}
	if *t.Puts+*t.Gets > maxCheckerActions {
		return fmt.Errorf("%s - tasks[%d] total PUT and GET count must not exceed 1024", location, index)
	}
	return nil
}

func (t Task) validateActionLimits(index int) error {
	const location = "config.Task.validateActionLimits"
	for key, value := range map[string]*int{"puts": t.Puts, "gets": t.Gets, "places": t.Places, "checker_timeout": t.CheckerTimeout, "default_score": t.DefaultScore, "get_period": t.GetPeriod} {
		minimum := 0
		if key == "places" || key == "checker_timeout" || key == "get_period" {
			minimum = 1
		}
		if value == nil || *value < minimum || *value > math.MaxInt32 {
			return fmt.Errorf("%s - tasks[%d].%s is outside its valid range", location, index, key)
		}
		if (key == "puts" || key == "gets") && *value > maxCheckerActions {
			return fmt.Errorf("%s - tasks[%d].%s must not exceed 1024", location, index, key)
		}
	}
	return nil
}

func ValidLogoPath(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > maxLogoLength || unsafePathCharacters(value) {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || !validLogoOrigin(u) {
		return false
	}
	decoded, err := unescapeLogoPath(u.EscapedPath())
	if err != nil || unsafePathCharacters(decoded) || strings.HasPrefix(decoded, "//") {
		return false
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func validLogoOrigin(u *url.URL) bool {
	if u.User != nil {
		return false
	}
	if u.Scheme != "" {
		return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
	}
	return u.Host == "" && strings.HasPrefix(u.Path, "/")
}

func unescapeLogoPath(value string) (string, error) {
	const location = "config.unescapeLogoPath"
	for {
		next, err := url.PathUnescape(value)
		if err != nil {
			return "", fmt.Errorf("%s - decode URL path: %w", location, err)
		}
		if next == value {
			return value, nil
		}
		value = next
	}
}

func unsafePathCharacters(value string) bool {
	if strings.Contains(value, "\\") {
		return true
	}
	for _, c := range value {
		if c < 32 || c == 127 {
			return true
		}
	}
	return false
}
