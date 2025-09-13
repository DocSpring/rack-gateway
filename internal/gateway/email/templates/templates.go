package templates

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	texttmpl "text/template"
)

const readmeURL = "https://github.com/DocSpring/convox-gateway/blob/main/README.md"

func WebURL(base string) string {
	b := strings.TrimRight(base, "/")
	return b + "/.gateway/web/"
}

// Welcome (new user)
var welcomeText = texttmpl.Must(texttmpl.New("welcome_text").Parse(
	"Convox Gateway ({{.Rack}})\n\n" +
		"Hi {{.Invitee}},\n\n" +
		"You’ve been granted access to the Convox Gateway for the {{.Rack}} rack.\n\n" +
		"Access the Web UI\n" +
		"{{.WebURL}}\n\n" +
		"Configure Convox Gateway CLI\n" +
		"------------------------------------------\n" +
		"Clone the repository and install the CLI:\n\n" +
		"git clone git@github.com:DocSpring/convox-gateway.git\n" +
		"cd convox-gateway\n" +
		"./scripts/install.sh\n\n" +
		"Authenticate the CLI against this gateway:\n\n" +
		"$ convox-gateway login {{.Rack}} {{.CLIBase}}\n\n" +
		"After logging in, you can run Convox commands via the gateway using convox-gateway convox …\n\n" +
		"See the README for more information:\n" +
		readmeURL + "\n\n" +
		"Added by {{.Inviter}}\n",
))

var welcomeHTML = template.Must(template.New("welcome_html").Parse(
	"<!DOCTYPE html><html><body style=\"font-family:Arial,Helvetica,sans-serif;color:#111;line-height:1.5;\">" +
		"<h2 style=\"margin:0 0 12px 0;\">Convox Gateway ({{.Rack}})</h2>" +
		"<p>Hi {{.Invitee}},</p>" +
		"<p>You’ve been granted access to the Convox Gateway for the <strong>{{.Rack}}</strong> rack.</p>" +
		"<h3 style=\"margin:20px 0 8px;\">Access the Web UI</h3>" +
		"<p><a href=\"{{.WebURL}}\" style=\"color:#0b5fff;text-decoration:none;\">{{.WebURL}}</a></p>" +
		"<h3 style=\"margin:20px 0 8px;\">Configure Convox Gateway CLI</h3>" +
		"<p>Clone the repository and install the CLI:</p>" +
		"<pre style=\"background:#f6f8fa;padding:12px;border-radius:6px;overflow:auto;\">git clone git@github.com:DocSpring/convox-gateway.git\ncd convox-gateway\n./scripts/install.sh</pre>" +
		"<p>Authenticate the CLI against this gateway:</p>" +
		"<pre style=\"background:#f6f8fa;padding:12px;border-radius:6px;overflow:auto;\">$ convox-gateway login {{.Rack}} {{.CLIBase}}</pre>" +
		"<p>After logging in, you can run Convox commands via the gateway using <code>convox-gateway convox …</code></p>" +
		fmt.Sprintf("<p>See the README for more information:<br/><a href=\"%s\" style=\"color:#0b5fff;text-decoration:none;\">%s</a></p>", readmeURL, readmeURL) +
		"<hr style=\"border:none;border-top:1px solid #e5e5e5;margin:24px 0;\"/>" +
		"<p style=\"font-size:12px;color:#555;\">Added by {{.Inviter}}</p>" +
		"</body></html>",
))

func RenderWelcome(rack, invitee, inviter, webBase, cliBase string) (text string, html string, err error) {
	data := map[string]string{
		"Rack":    rack,
		"Invitee": invitee,
		"Inviter": inviter,
		"WebURL":  WebURL(webBase),
		"CLIBase": cliBase,
	}
	var tb, hb bytes.Buffer
	if err = welcomeText.Execute(&tb, data); err != nil {
		return
	}
	if err = welcomeHTML.Execute(&hb, data); err != nil {
		return
	}
	return tb.String(), hb.String(), nil
}

// Settings changed
var settingsText = texttmpl.Must(texttmpl.New("settings_text").Parse(
	"Convox Gateway ({{.Rack}})\n\n" +
		"{{.Actor}} updated the {{.Key}} setting.\n\n" +
		"New value:\n{{.Value}}\n",
))

var settingsHTML = template.Must(template.New("settings_html").Parse(
	"<!DOCTYPE html><html><body style=\"font-family:Arial,Helvetica,sans-serif;color:#111;line-height:1.5;\">" +
		"<h2 style=\"margin:0 0 12px 0;\">Convox Gateway ({{.Rack}})</h2>" +
		"<p><strong>{{.Actor}}</strong> updated the <code>{{.Key}}</code> setting.</p>" +
		"<p>New value:</p>" +
		"<pre style=\"background:#f6f8fa;padding:12px;border-radius:6px;overflow:auto;\">{{.Value}}</pre>" +
		"</body></html>",
))

func RenderSettingsChanged(rack, actor, key, value string) (string, string, error) {
	data := map[string]string{"Rack": rack, "Actor": actor, "Key": key, "Value": value}
	var tb, hb bytes.Buffer
	if err := settingsText.Execute(&tb, data); err != nil {
		return "", "", err
	}
	if err := settingsHTML.Execute(&hb, data); err != nil {
		return "", "", err
	}
	return tb.String(), hb.String(), nil
}

// Token created
var tokenOwnerText = texttmpl.Must(texttmpl.New("token_owner_text").Parse(
	"Convox Gateway ({{.Rack}})\n\n" +
		"A new API token '{{.Name}}' was created for your account.\n" +
		"Created by: {{.Creator}}\n\n" +
		"If this wasn't expected, please contact an admin.\n",
))
var tokenOwnerHTML = template.Must(template.New("token_owner_html").Parse(
	"<!DOCTYPE html><html><body style=\"font-family:Arial,Helvetica,sans-serif;color:#111;line-height:1.5;\">" +
		"<h2 style=\"margin:0 0 12px 0;\">Convox Gateway ({{.Rack}})</h2>" +
		"<p>A new API token '<strong>{{.Name}}</strong>' was created for your account.</p>" +
		"<p>Created by: {{.Creator}}</p>" +
		"<p>If this wasn't expected, please contact an admin.</p>" +
		"</body></html>",
))

func RenderTokenCreatedOwner(rack, name, creator string) (string, string, error) {
	data := map[string]string{"Rack": rack, "Name": name, "Creator": creator}
	var tb, hb bytes.Buffer
	if err := tokenOwnerText.Execute(&tb, data); err != nil {
		return "", "", err
	}
	if err := tokenOwnerHTML.Execute(&hb, data); err != nil {
		return "", "", err
	}
	return tb.String(), hb.String(), nil
}

var tokenAdminText = texttmpl.Must(texttmpl.New("token_admin_text").Parse(
	"Convox Gateway ({{.Rack}})\n\n" +
		"An API token '{{.Name}}' was created for {{.Owner}}.\n" +
		"Created by: {{.Creator}}\n",
))
var tokenAdminHTML = template.Must(template.New("token_admin_html").Parse(
	"<!DOCTYPE html><html><body style=\"font-family:Arial,Helvetica,sans-serif;color:#111;line-height:1.5;\">" +
		"<h2 style=\"margin:0 0 12px 0;\">Convox Gateway ({{.Rack}})</h2>" +
		"<p>An API token '<strong>{{.Name}}</strong>' was created for {{.Owner}}.</p>" +
		"<p>Created by: {{.Creator}}</p>" +
		"</body></html>",
))

func RenderTokenCreatedAdmin(rack, name, owner, creator string) (string, string, error) {
	data := map[string]string{"Rack": rack, "Name": name, "Owner": owner, "Creator": creator}
	var tb, hb bytes.Buffer
	if err := tokenAdminText.Execute(&tb, data); err != nil {
		return "", "", err
	}
	if err := tokenAdminHTML.Execute(&hb, data); err != nil {
		return "", "", err
	}
	return tb.String(), hb.String(), nil
}

// User added (admin notice)
var userAddedAdminText = texttmpl.Must(texttmpl.New("user_added_admin_text").Parse(
	"Convox Gateway ({{.Rack}})\n\n" +
		"{{.Actor}} added new user {{.Email}} ({{.Name}}) with roles: {{.Roles}}.\n",
))
var userAddedAdminHTML = template.Must(template.New("user_added_admin_html").Parse(
	"<!DOCTYPE html><html><body style=\"font-family:Arial,Helvetica,sans-serif;color:#111;line-height:1.5;\">" +
		"<h2 style=\"margin:0 0 12px 0;\">Convox Gateway ({{.Rack}})</h2>" +
		"<p>{{.Actor}} added new user <strong>{{.Email}}</strong> ({{.Name}}) with roles: {{.Roles}}.</p>" +
		"</body></html>",
))

func RenderUserAddedAdmin(rack, actor, email, name string, roles []string) (string, string, error) {
	data := map[string]string{"Rack": rack, "Actor": actor, "Email": email, "Name": name, "Roles": strings.Join(roles, ", ")}
	var tb, hb bytes.Buffer
	if err := userAddedAdminText.Execute(&tb, data); err != nil {
		return "", "", err
	}
	if err := userAddedAdminHTML.Execute(&hb, data); err != nil {
		return "", "", err
	}
	return tb.String(), hb.String(), nil
}
