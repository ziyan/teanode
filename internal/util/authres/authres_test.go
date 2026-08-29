package authres_test

import (
	"github.com/ziyan/teanode/internal/util/authres"
)

type msgauthTest struct {
	value      string
	identifier string
	results    []authres.Result
}

var msgauthTests = []msgauthTest{
	{
		value:      "example.org; none",
		identifier: "example.org",
		results:    nil,
	},
	{
		value:      "example.com;\r\n dkim=none ",
		identifier: "example.com",
		results: []authres.Result{
			&authres.DKIMResult{Value: authres.ResultNone},
		},
	},
	{
		value: "example.com;\r\n" +
			" spf=pass smtp.mailfrom=example.net",
		identifier: "example.com",
		results: []authres.Result{
			&authres.SPFResult{Value: authres.ResultPass, From: "example.net"},
		},
	},
	{
		value: "example.com;\r\n" +
			" spf=fail reason=bad smtp.mailfrom=example.net",
		identifier: "example.com",
		results: []authres.Result{
			&authres.SPFResult{Value: authres.ResultFail, Reason: "bad", From: "example.net"},
		},
	},
	{
		value: "example.com;\r\n" +
			" auth=pass smtp.auth=sender@example.com;\r\n" +
			" spf=pass smtp.mailfrom=example.com",
		identifier: "example.com",
		results: []authres.Result{
			&authres.AuthResult{Value: authres.ResultPass, Auth: "sender@example.com"},
			&authres.SPFResult{Value: authres.ResultPass, From: "example.com"},
		},
	},
	{
		value: "example.com;\r\n" +
			" sender-id=pass header.from=example.com",
		identifier: "example.com",
		results: []authres.Result{
			&authres.SenderIDResult{Value: authres.ResultPass, HeaderKey: "from", HeaderValue: "example.com"},
		},
	},
	{
		value: "example.com;\r\n" +
			" sender-id=hardfail header.from=example.com;\r\n" +
			" dkim=pass header.i=sender@example.com",
		identifier: "example.com",
		results: []authres.Result{
			&authres.SenderIDResult{Value: authres.ResultHardFail, HeaderKey: "from", HeaderValue: "example.com"},
			&authres.DKIMResult{Value: authres.ResultPass, Identifier: "sender@example.com"},
		},
	},
	{
		value: "example.com;\r\n" +
			" auth=pass smtp.auth=sender@example.com;\r\n" +
			" spf=hardfail smtp.mailfrom=example.com",
		identifier: "example.com",
		results: []authres.Result{
			&authres.AuthResult{Value: authres.ResultPass, Auth: "sender@example.com"},
			&authres.SPFResult{Value: authres.ResultHardFail, From: "example.com"},
		},
	},
	{
		value: "example.com;\r\n" +
			" dkim=pass header.i=@mail-router.example.net;\r\n" +
			" dkim=fail header.i=@newyork.example.com",
		identifier: "example.com",
		results: []authres.Result{
			&authres.DKIMResult{Value: authres.ResultPass, Identifier: "@mail-router.example.net"},
			&authres.DKIMResult{Value: authres.ResultFail, Identifier: "@newyork.example.com"},
		},
	},
}
