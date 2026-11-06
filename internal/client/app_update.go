package client

import "context"

// AppUpdate is the newest iPhone app on a channel.
type AppUpdate struct {
	UpdateChannel string `json:"updateChannel"`
	Version       string `json:"version"`
	BuildNumber   string `json:"buildNumber"`
	InstallURL    string `json:"installUrl"`
}

// GetAppUpdate is the newest iPhone app on a channel, testflight or appstore,
// nil when there is none to offer.
func GetAppUpdate(ctx context.Context, connection *Client, updateChannel string) (*AppUpdate, error) {
	var result struct {
		GetAppUpdate *AppUpdate `json:"GetAppUpdate"`
	}
	query := `query ($updateChannel: String!) { GetAppUpdate(updateChannel: $updateChannel) { updateChannel version buildNumber installUrl } }`
	if err := connection.Execute(ctx, query, map[string]any{"updateChannel": updateChannel}, &result); err != nil {
		return nil, err
	}
	return result.GetAppUpdate, nil
}
