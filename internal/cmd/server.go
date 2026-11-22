package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewServerCommand builds "teanode server": the running instance, as opposed
// to its configuration.
func NewServerCommand() *cli.Command {
	return &cli.Command{
		Name:  "server",
		Usage: "the running instance: its status, its addresses, and restarting it",
		Commands: []*cli.Command{
			{
				Name:   "status",
				Usage:  "which build is running, for how long, and whether a restart is needed",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runServerStatus,
			},
			{
				Name:  "restart",
				Usage: "restart the instance, which is how a change to the listeners, TLS, storage or the integrations takes effect",
				Description: "The process exits and whatever supervises it — a container's restart\n" +
					"policy, systemd — is expected to start a new one. The server refuses when\n" +
					"it was started in a way that has no supervisor, rather than going down and\n" +
					"staying down.",
				Flags:  []cli.Flag{ForceFlag(), JSONFlag()},
				Action: runServerRestart,
			},
			{
				Name:   "addresses",
				Usage:  "the addresses the outside world reaches this server on, which its DNS records must point at",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runServerAddresses,
			},
			{
				Name:  "storage",
				Usage: "what the database and the files hold, its growth, and when a disk fills",
				Description: "The database by area, the files by kind or by person, a line fitted through\n" +
					"the last thirty days of daily samples, and each measured disk with the day\n" +
					"it fills at that rate. A disk that fills within thirty days is marked, as the\n" +
					"operators are warned. Needs server:manage.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "by", Usage: "area (the database), kind (the files) or person (the files); area by default"},
					&cli.StringFlag{Name: "since", Usage: "history from when, as a date or a duration like 72h; the last thirty days by default"},
					&cli.BoolFlag{Name: "history", Usage: "also list each day's sample"},
				},
				Action: runServerStorage,
			},
			{
				Name:  "identity",
				Usage: "how outgoing mail identifies itself, and whether a strict receiver would believe it",
				Description: "The address mail leaves from, whether its reverse name resolves back to\n" +
					"it, and whether the SMTP greeting agrees. Large receivers refuse mail\n" +
					"outright when these disagree, which is a different question from whether\n" +
					"a domain's records are published.",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runServerIdentity,
			},
		},
	}
}

func runServerStatus(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	status, err := client.GetServerStatus(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(status)
	}

	pending := "nothing; every setting in use is current"
	if len(status.PendingRestart) > 0 {
		pending = strings.Join(status.PendingRestart, ", ") + " changed since it started"
	}
	restarting := ""
	if status.Restarting {
		restarting = " (restart under way)"
	}
	return printFields([][2]string{
		{"instance", status.Instance},
		{"version", status.Version},
		{"commit", status.Commit},
		{"started", status.StartedAt.Local().Format(time.RFC3339)},
		{"uptime", (time.Duration(status.UptimeSeconds) * time.Second).String()},
		{"supervision", status.Supervision + restarting},
		{"needs restart for", pending},
	})
}

func runServerStorage(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	since, err := parseSince(command.String("since"))
	if err != nil {
		return err
	}
	usage, err := client.ReadStorageUsage(ctx, connection, since, command.String("by"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(usage)
	}
	return printStorageUsage(command, usage)
}

// storageWords are the kinds of file as an operator reads them.
var storageWords = map[string]string{
	"photoOriginal":     "photo originals",
	"photoPreview":      "photo previews",
	"recordingOriginal": "recording originals",
	"recordingDerived":  "recording audio",
	"mail":              "mail messages",
	"agentAttachment":   "agent attachments",
	"dataDirectory":     "data directory",
	"fileDirectory":     "file storage",
	"objectStore":       "object store",
}

func storageWordsFor(key string) string {
	if words, isKnown := storageWords[key]; isKnown {
		return words
	}
	return key
}

// describeStorageBytes writes a size in 1024-based units, one decimal from a
// megabyte up, as the server's own warning does.
func describeStorageBytes(sizeBytes int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	size := float64(sizeBytes)
	index := 0
	for size >= 1024 && index < len(units)-1 {
		size /= 1024
		index++
	}
	if index < 2 {
		return fmt.Sprintf("%.0f %s", size, units[index])
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
}

func printStorageUsage(command *cli.Command, usage *client.StorageUsage) error {
	for _, disk := range usage.Disks {
		if disk.IsProjectedFull && disk.ProjectedFullOn != nil && disk.DaysUntilFull != nil {
			fmt.Printf("warning: the %s disk is projected to fill in about %.0f days, on %s\n", storageWordsFor(disk.DiskLabel), *disk.DaysUntilFull, *disk.ProjectedFullOn)
		}
	}
	growth := "not enough samples yet"
	if usage.Growth.IsGrowthProjectable {
		growth = fmt.Sprintf("%s a day over %d days of samples", describeStorageBytes(int64(usage.Growth.GrowthBytesPerDay)), usage.Growth.GrowthSampleCount)
	}
	fields := [][2]string{
		{"database", describeStorageBytes(usage.DatabaseSizeBytes)},
		{"files", describeStorageBytes(usage.FileSizeBytes)},
		{"total", describeStorageBytes(usage.TotalSizeBytes)},
		{"growth", growth},
	}
	if usage.Growth.IsGrowthProjectable {
		fields = append(fields,
			[2]string{"in 30 days", describeStorageBytes(usage.Growth.ProjectedSize30DaysBytes)},
			[2]string{"in 90 days", describeStorageBytes(usage.Growth.ProjectedSize90DaysBytes)},
			[2]string{"in 365 days", describeStorageBytes(usage.Growth.ProjectedSize365DaysBytes)})
		if !usage.Growth.AreFilesCounted {
			fields = append(fields, [2]string{"", "the files are in an object store and are not counted in the growth"})
		}
	}
	if err := printFields(fields); err != nil {
		return err
	}

	fmt.Println()
	diskRows := [][]string{}
	for _, disk := range usage.Disks {
		if !disk.IsDiskMeasured || disk.DiskTotalBytes == nil || disk.DiskFreeBytes == nil || disk.DiskUsedBytes == nil {
			diskRows = append(diskRows, []string{storageWordsFor(disk.DiskLabel), "not measured", "", "", ""})
			continue
		}
		fullOn := ""
		if disk.ProjectedFullOn != nil {
			fullOn = *disk.ProjectedFullOn
		}
		diskRows = append(diskRows, []string{storageWordsFor(disk.DiskLabel), describeStorageBytes(*disk.DiskTotalBytes), describeStorageBytes(*disk.DiskUsedBytes), describeStorageBytes(*disk.DiskFreeBytes), fullOn})
	}
	if err := printTable([]string{"DISK", "TOTAL", "USED", "FREE", "FULL ON"}, diskRows); err != nil {
		return err
	}

	fmt.Println()
	rows := [][]string{}
	for _, row := range usage.UsageRows {
		label := row.RowLabel
		switch usage.BreakdownBy {
		case "kind":
			label = storageWordsFor(row.RowKey)
		case "person":
			if row.RowKey == "" {
				label = "not tied to a person"
			}
		}
		rows = append(rows, []string{label, describeStorageBytes(row.RowSizeBytes)})
	}
	if err := printTable([]string{strings.ToUpper(usage.BreakdownBy), "SIZE"}, rows); err != nil {
		return err
	}

	if command.Bool("history") {
		fmt.Println()
		days := [][]string{}
		for _, day := range usage.HistoryDays {
			days = append(days, []string{day.SampledOn, describeStorageBytes(day.DatabaseSizeBytes), describeStorageBytes(day.FileSizeBytes), describeStorageBytes(day.TotalSizeBytes)})
		}
		return printTable([]string{"DAY", "DATABASE", "FILES", "TOTAL"}, days)
	}
	return nil
}

func runServerRestart(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	status, err := client.GetServerStatus(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if err := confirm(command, fmt.Sprintf("This restarts instance %s; mail is refused for the few seconds it takes.", status.Instance)); err != nil {
		return err
	}
	result, err := client.RestartServer(ctx, connection)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(result)
	}
	if !result.Started {
		fmt.Printf("instance %s is already restarting\n", result.Instance)
		return nil
	}
	fmt.Printf("instance %s is restarting; %s is expected to bring it back\n", result.Instance, result.Supervision)
	return nil
}

func runServerAddresses(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	addresses, err := client.GetServerAddresses(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(addresses)
	}
	fields := [][2]string{}
	if addresses.IPv4 != "" {
		fields = append(fields, [2]string{"ipv4", addresses.IPv4})
	}
	if addresses.IPv6 != "" {
		fields = append(fields, [2]string{"ipv6", addresses.IPv6})
	}
	if addresses.Error != "" {
		fields = append(fields, [2]string{"problem", addresses.Error})
	}
	if len(fields) == 0 {
		fmt.Println("the server has not worked out its external addresses yet")
		return nil
	}
	return printFields(fields)
}

func runServerIdentity(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	identity, err := client.GetOutgoingIdentity(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(identity)
	}
	confirmed := "no: the reverse name does not resolve back to the address"
	if identity.Confirmed {
		confirmed = "yes"
	}
	helloMatches := "no: the greeting name does not resolve to the address"
	if identity.HelloMatches {
		helloMatches = "yes"
	}
	return printFields([][2]string{
		{"leaves from", identity.Address},
		{"via", identity.Via},
		{"reverse name", identity.ReverseName},
		{"resolves back", strings.Join(identity.ForwardAddresses, ", ")},
		{"confirmed", confirmed},
		{"greeting", identity.HelloName},
		{"greeting resolves to", strings.Join(identity.HelloAddresses, ", ")},
		{"greeting matches", helloMatches},
	})
}
