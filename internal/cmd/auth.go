package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewAuthCommand builds "teanode auth": signing in to a server and keeping
// the profiles that result.
func NewAuthCommand() *cli.Command {
	return &cli.Command{
		Name:  "auth",
		Usage: "sign in to a server, and manage the servers you are signed in to",
		Description: "A profile is a server and the token that acts as you there. Signing in\n" +
			"saves one and makes it the one every other command talks to; --profile\n" +
			"picks another for one command. Profiles are kept in\n" +
			"~/.config/teanode/profiles.json, readable only by you.",
		Commands: []*cli.Command{
			{
				Name:  "login",
				Usage: "sign in to a server and save it as a profile",
				Description: "Opens the dashboard in a browser. Sign in there if you are not already,\n" +
					"press Authorize, and the token comes back to this command over a loopback\n" +
					"connection — nothing passes through the clipboard or the shell history.\n\n" +
					"If a browser is not an option, paste a token instead: --token takes one,\n" +
					"and --token - asks for it without echoing.\n\n" +
					"  teanode auth login --url https://mail.example.com\n" +
					"  teanode auth login --url https://mail.example.com --no-browser\n" +
					"  teanode auth login --url https://mail.example.com --token -\n" +
					"  teanode auth login --url https://staging.example.com --name staging",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "url",
						Usage: "the server, for example https://mail.example.com; may be omitted when re-signing in to a saved profile",
					},
					&cli.StringFlag{
						Name:  "name",
						Usage: "what to call the profile; the server's host name unless given",
					},
					&cli.StringFlag{
						Name:  "token",
						Usage: "use this token instead of the browser; \"-\" reads it without echoing",
					},
					&cli.BoolFlag{
						Name:  "no-browser",
						Usage: "print the address to open instead of opening it",
					},
					&cli.StringFlag{
						Name:  "lifetime",
						Usage: "how long the token issued by the browser lasts, for example 720h; omit for one that does not expire",
					},
					&cli.BoolFlag{
						Name:  "insecure",
						Usage: "do not verify the server's certificate, and remember that in the profile",
					},
					&cli.BoolFlag{
						Name:  "read-only",
						Usage: "save a profile that can look but not change anything; for a script or an agent",
					},
				},
				Action: runAuthLogin,
			},
			{
				Name:      "logout",
				Usage:     "revoke the profile's token on the server and forget the profile",
				ArgsUsage: "[profile]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "keep-token",
						Usage: "forget the profile without revoking its token",
					},
				},
				Action: runAuthLogout,
			},
			{
				Name:   "status",
				Usage:  "say which server the next command would talk to, and as whom",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAuthStatus,
			},
			{
				Name:   "list",
				Usage:  "list the saved profiles",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAuthList,
			},
			{
				Name:      "switch",
				Aliases:   []string{"use"},
				Usage:     "make a saved profile the one every command talks to",
				ArgsUsage: "<profile>",
				Action:    runAuthSwitch,
			},
			{
				Name:      "remove",
				Usage:     "forget a profile without revoking its token",
				ArgsUsage: "<profile>",
				Action:    runAuthRemove,
			},
			{
				Name:      "set-read-only",
				Usage:     "make a saved profile refuse changes, or allow them again",
				ArgsUsage: "<profile> <true|false>",
				Description: "A read-only profile refuses every change before it is sent, on this\n" +
					"machine, so a script or an agent given the profile can look but not\n" +
					"touch. The token itself is unchanged; the server would accept the\n" +
					"change, and this profile does not ask it to.\n\n" +
					"  teanode auth set-read-only mail.example.com true\n\n" +
					"TEANODE_READ_ONLY=1 in the environment, or --read-only on a command,\n" +
					"does the same for one shell or one command, and no profile setting\n" +
					"overrides it.",
				Action: runAuthSetReadOnly,
			},
		},
	}
}

func runAuthLogin(ctx context.Context, command *cli.Command) error {
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}

	serverUrl := command.String("url")
	if serverUrl == "" {
		serverUrl = command.Root().String("url")
	}
	name := command.String("name")
	insecure := command.Bool("insecure") || command.Root().Bool("insecure")
	readOnly := command.Bool("read-only") || command.Root().Bool("read-only")

	// Signing in again to a saved profile need not repeat its address. With
	// neither an address nor a name, the profile meant is the one --profile
	// names, or else the active one: the common case is a token that has
	// expired on the server somebody was already using. It is said which,
	// because the profile's token is about to be replaced.
	if serverUrl == "" && name == "" {
		name = command.Root().String("profile")
		if name == "" {
			name = profiles.Active
		}
		if name == LocalProfileName {
			return usage("the console is not signed in to; usage: teanode auth login --url https://mail.example.com")
		}
		if name == "" {
			return usage("which server? usage: teanode auth login --url https://mail.example.com")
		}
		if profiles.Find(name) == nil {
			return usage(fmt.Sprintf("no profile called %q to sign in to again; usage: teanode auth login --url https://mail.example.com", name))
		}
		fmt.Printf("Signing in again to profile %q, %s.\n", name, profiles.Find(name).URL)
	}
	if name == LocalProfileName {
		return usage(fmt.Sprintf("%q is the name of the console and cannot be a saved profile; pass --name", LocalProfileName))
	}
	existing := profiles.Find(name)
	if existing != nil && serverUrl == "" {
		serverUrl = existing.URL
	}
	if serverUrl == "" {
		return usage("which server? usage: teanode auth login --url https://mail.example.com")
	}
	serverUrl = client.NormalizeURL(serverUrl)
	if name == "" {
		name = hostOf(serverUrl)
		existing = profiles.Find(name)
	}

	// A profile signed in to again keeps its certificate posture and its
	// read-only bit unless told otherwise, so that a look-only profile does
	// not become a writable one because its token expired.
	if existing != nil {
		if !command.IsSet("insecure") {
			insecure = insecure || existing.Insecure
		}
		if !command.IsSet("read-only") && !command.Root().IsSet("read-only") {
			readOnly = existing.ReadOnly
		}
	}

	profile := &Profile{Name: name, URL: serverUrl, Insecure: insecure, ReadOnly: readOnly}
	if command.IsSet("token") {
		token := strings.TrimSpace(command.String("token"))
		if token == "-" {
			token, err = ReadSecret("token: ")
			if err != nil {
				return err
			}
		}
		if token == "" {
			return fmt.Errorf("no token was given")
		}
		profile.Token = token
		profile.TokenID = tokenIdOf(token)
	} else {
		result, err := browserLogin(ctx, serverUrl, name, command.String("lifetime"), !command.Bool("no-browser"))
		if err != nil {
			return err
		}
		profile.Token = result.Token
		profile.TokenID = result.TokenID
		profile.Username = result.Username
	}

	// Confirm the token works before saving anything, and learn whom it acts
	// as from the server rather than trusting the page.
	connection, err := client.New(client.Options{URL: profile.URL, Token: profile.Token, Insecure: profile.Insecure})
	if err != nil {
		return err
	}
	current, err := client.GetCurrentUser(ctx, connection)
	if err != nil {
		// Described here, where the server it was tried against is known;
		// the general advice would be about whichever server the root flags
		// name, which is not this one.
		return &describedError{fmt.Errorf("the token did not work against %s: %w. Sign in again, or check the token", profile.URL, err)}
	}
	if current != nil {
		profile.Username = current.Username
	}

	profiles.Set(profile)
	if err := profiles.Save(); err != nil {
		return err
	}

	// Re-signing in replaced the profile's token. The old one is revoked so
	// that it does not live on, unseen from here, in the server's list —
	// after the new one is safely saved, so that a failed save never leaves
	// the profile holding a token that has just been revoked.
	if existing != nil {
		revokeReplacedToken(ctx, command, existing, profile)
	}

	who := profile.Username
	if who == "" {
		who = "the console"
	}
	fmt.Printf("Signed in to %s as %s; saved profile %q, now active.\n", profile.URL, who, profile.Name)
	if profile.Insecure {
		fmt.Printf("This profile does not verify the server's certificate.\n")
	}
	if profile.ReadOnly {
		fmt.Printf("This profile is read-only: commands through it can look but not change anything.\n")
	}
	return nil
}

// revokeReplacedToken revokes the token a profile held before a new sign-in
// replaced it, and says so, because the old token may have been copied
// somewhere that is still using it. Best effort: it may already be gone,
// which is why somebody signed in again, and the new one works either way.
//
// Not on a read-only target: a revocation is a change, and the switches
// that refuse changes are not talked around here either. The old token is
// named instead, for revoking by hand.
func revokeReplacedToken(ctx context.Context, command *cli.Command, existing, replacement *Profile) {
	if existing.TokenID == "" || existing.TokenID == replacement.TokenID || existing.URL != replacement.URL {
		return
	}
	if replacement.ReadOnly || command.Root().Bool("read-only") {
		fmt.Printf("The previous token %s is still valid, and this connection is read-only; revoke it with 'teanode token revoke %s' or from the dashboard.\n",
			existing.TokenID, existing.TokenID)
		return
	}
	connection, err := client.New(client.Options{URL: existing.URL, Token: existing.Token, Insecure: existing.Insecure})
	if err != nil {
		return
	}
	if err := client.DeleteToken(ctx, connection, existing.TokenID); err != nil {
		fmt.Printf("The previous token %s could not be revoked (%s); revoke it with 'teanode token revoke %s' if it is still listed.\n",
			existing.TokenID, err, existing.TokenID)
		return
	}
	fmt.Printf("Revoked the previous token %s.\n", existing.TokenID)
}

// browserLogin runs the loopback handshake described in loopback.go.
func browserLogin(ctx context.Context, serverUrl, name, lifetime string, open bool) (*loginResult, error) {
	listener, err := newLoopback(ctx, serverUrl)
	if err != nil {
		return nil, err
	}
	defer listener.Close()

	authorizeUrl := listener.AuthorizeURL(serverUrl, name, lifetime)
	fmt.Printf("Authorize this command in the browser:\n\n  %s\n\n", authorizeUrl)
	if open {
		if err := openBrowser(authorizeUrl); err != nil {
			fmt.Printf("(a browser could not be opened; open the address above yourself)\n\n")
		}
	}
	fmt.Printf("Waiting for the browser...\n")
	return listener.Wait(ctx, loginTimeout)
}

// tokenIdOf reads the identifier out of a token string, so that a pasted
// token can be revoked on logout like one the browser handed over.
//
// A token is its prefix, a 26 character identifier, a 16 character key and a
// 16 character signature. The identifier is not secret — it is what the
// server's own list shows — and reading it here verifies nothing; the server
// does that when the token is used. Anything not shaped like a token yields
// nothing, and logout then leaves revocation to the dashboard.
func tokenIdOf(token string) string {
	rest, found := strings.CutPrefix(token, "tnt_")
	if !found || len(rest) != 26+16+16 {
		return ""
	}
	return strings.ToLower(rest[:26])
}

func runAuthLogout(ctx context.Context, command *cli.Command) error {
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}
	name := command.Args().First()
	if name == "" {
		name = profiles.Active
	}
	if name == "" {
		return fmt.Errorf("not signed in to any server")
	}
	profile := profiles.Find(name)
	if profile == nil {
		return fmt.Errorf("no profile called %q; 'teanode auth list' shows them", name)
	}

	// Revoked whether or not the profile is read-only: forgetting a profile
	// and leaving its token live is the worse outcome, and signing out is
	// the reader's own act.
	if !command.Bool("keep-token") && profile.TokenID != "" {
		connection, err := client.New(client.Options{URL: profile.URL, Token: profile.Token, Insecure: profile.Insecure})
		if err != nil {
			return err
		}
		if err := client.DeleteToken(ctx, connection, profile.TokenID); err != nil {
			// Forgetting the profile is still the right thing to do; a
			// token that could not be revoked is named so it can be revoked
			// from the dashboard.
			fmt.Printf("could not revoke token %s on %s: %s\n", profile.TokenID, profile.URL, err)
		}
	}

	profiles.Remove(name)
	if err := profiles.Save(); err != nil {
		return err
	}
	fmt.Printf("Signed out of %s; forgot profile %q.\n", profile.URL, name)
	if profiles.Active != "" {
		fmt.Printf("The active profile is now %q.\n", profiles.Active)
	}
	return nil
}

func runAuthStatus(ctx context.Context, command *cli.Command) error {
	resolved, err := resolveCommandTarget(command)
	if err != nil {
		return err
	}

	// Whether the server answers, and as whom. A quick probe rather than a
	// long one: this is the command somebody runs to find out why the last
	// one failed.
	probeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var username string
	var reachable bool
	var problem string
	connection, err := openClient(command)
	if err != nil {
		problem = err.Error()
	} else {
		current, err := client.GetCurrentUser(probeContext, connection)
		switch {
		case err != nil:
			problem = err.Error()
		case current != nil:
			reachable, username = true, current.Username
		default:
			reachable, username = true, "the console"
		}
	}

	if command.Bool("json") {
		return PrintJSON(map[string]any{
			"profile":   resolved.Profile,
			"url":       resolved.URL,
			"local":     resolved.Local,
			"insecure":  resolved.Insecure,
			"readOnly":  resolved.ReadOnly,
			"reachable": reachable,
			"username":  username,
			"error":     problem,
		})
	}

	fmt.Printf("Server:   %s\n", describeTarget(resolved))
	if reachable {
		fmt.Printf("Acts as:  %s\n", username)
	} else {
		fmt.Printf("Problem:  %s\n", problem)
	}
	if resolved.ReadOnly {
		fmt.Printf("Access:   read-only; changes are refused before they are sent\n")
	}
	if resolved.Insecure {
		fmt.Printf("Warning:  the server's certificate is not verified\n")
	}
	return nil
}

func runAuthList(ctx context.Context, command *cli.Command) error {
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}

	if command.Bool("json") {
		type listed struct {
			Name     string `json:"name"`
			URL      string `json:"url"`
			Username string `json:"username,omitempty"`
			TokenID  string `json:"tokenId,omitempty"`
			Insecure bool   `json:"insecure"`
			ReadOnly bool   `json:"readOnly"`
			Active   bool   `json:"active"`
		}
		listing := make([]listed, 0, len(profiles.Profiles))
		for _, name := range profiles.Names() {
			profile := profiles.Profiles[name]
			listing = append(listing, listed{
				Name: name, URL: profile.URL, Username: profile.Username, TokenID: profile.TokenID,
				Insecure: profile.Insecure, ReadOnly: profile.ReadOnly, Active: name == profiles.Active,
			})
		}
		return PrintJSON(listing)
	}

	if len(profiles.Profiles) == 0 {
		fmt.Println("no profiles; sign in to a server with 'teanode auth login --url https://mail.example.com'")
		return nil
	}
	rows := make([][]string, 0, len(profiles.Profiles))
	for _, name := range profiles.Names() {
		profile := profiles.Profiles[name]
		active := ""
		if name == profiles.Active {
			active = "*"
		}
		insecure := ""
		if profile.Insecure {
			insecure = "yes"
		}
		readOnly := ""
		if profile.ReadOnly {
			readOnly = "yes"
		}
		rows = append(rows, []string{active, name, profile.URL, profile.Username, readOnly, insecure})
	}
	return printTable([]string{"", "NAME", "URL", "ACTS AS", "READ ONLY", "INSECURE"}, rows)
}

func runAuthSwitch(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which profile? usage: teanode auth switch <profile>")
	}
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}
	if profiles.Find(name) == nil {
		return fmt.Errorf("no profile called %q; 'teanode auth list' shows them", name)
	}
	profiles.Active = name
	if err := profiles.Save(); err != nil {
		return err
	}
	fmt.Printf("Commands now talk to %s (profile %q).\n", profiles.Find(name).URL, name)
	return nil
}

func runAuthRemove(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which profile? usage: teanode auth remove <profile>")
	}
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}
	profile := profiles.Find(name)
	if profile == nil {
		return fmt.Errorf("no profile called %q; 'teanode auth list' shows them", name)
	}
	profiles.Remove(name)
	if err := profiles.Save(); err != nil {
		return err
	}
	fmt.Printf("Forgot profile %q. Its token is still valid on %s", name, profile.URL)
	if profile.TokenID != "" {
		fmt.Printf("; revoke it with 'teanode token revoke %s' or from the dashboard", profile.TokenID)
	}
	fmt.Println(".")
	return nil
}

func runAuthSetReadOnly(ctx context.Context, command *cli.Command) error {
	name, value := command.Args().Get(0), command.Args().Get(1)
	if name == "" || value == "" {
		return usage("usage: teanode auth set-read-only <profile> <true|false>")
	}
	readOnly, err := strconv.ParseBool(value)
	if err != nil {
		return usage(fmt.Sprintf("%q is not true or false; usage: teanode auth set-read-only <profile> <true|false>", value))
	}
	profiles, err := LoadProfiles()
	if err != nil {
		return err
	}
	profile := profiles.Find(name)
	if profile == nil {
		return usage(fmt.Sprintf("no profile called %q; 'teanode auth list' shows them", name))
	}
	profile.ReadOnly = readOnly
	if err := profiles.Save(); err != nil {
		return err
	}
	if readOnly {
		fmt.Printf("Profile %q is read-only: commands through it can look but not change anything.\n", name)
	} else {
		fmt.Printf("Profile %q can make changes again.\n", name)
	}
	return nil
}
