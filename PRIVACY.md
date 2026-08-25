# Privacy policy

LongHub Manager is a local Windows application for managing the user's own
OpenClaw installation. It does not require a LongHub account, contain Cloud
Skill code, collect analytics or telemetry, or upload workspaces, conversations,
credentials, configuration files or backups to LongHub services. The optional
Manager Agent has a separate, user-configured model data flow described below.

This program will not transfer any information to other networked systems
unless specifically requested by the user or the person installing or operating
it.

## Network access

When the user selects **Check for updates**, Manager requests the public signed
LongHub Manager release manifest from `https://154-9-26-158.sslip.io`. When the
user separately confirms download or installation, Manager downloads the exact
installer named by that signed manifest. These requests contain ordinary HTTP
transport metadata such as the source IP address, as is inherent to an HTTPS
connection. Manager does not add an account identifier, device token, OpenClaw
configuration or user content to these requests.

When the user configures and uses **Manager Agent**, Manager sends the Agent
system instructions, the user's Agent messages, tool definitions and the
redacted tool results needed to answer the request to the model endpoint chosen
by the user. The model API key is sent only to that endpoint as an Authorization
credential. Manager rejects HTTP redirects for model requests so the credential
is not forwarded to another endpoint. LongHub does not receive this traffic
unless the user explicitly configures a LongHub-operated endpoint. The chosen
model provider's retention, training and privacy terms apply.

Commands that the user asks Manager to run may start the separately installed
OpenClaw application. OpenClaw and any providers, channels, plugins or skills
configured by the user have their own network behavior and privacy terms;
LongHub Manager neither configures nor proxies those external services.

## Local data

Manager stores local recovery state, signed-update state and configuration
backups under the current Windows user's application-data directories. The
loopback management token is generated locally, passed only in a browser URL
fragment and is not written to Manager logs. Manager Agent stores its model
address and model ID in the LongHub configuration directory; on Windows its API
key is stored separately in Windows Credential Manager. Agent conversations are
kept only in process memory and are not restored after Manager restarts.
Uninstalling Manager removes the application but intentionally preserves
user-created backups and native OpenClaw data unless the user removes them
separately.

## Contact

Questions and security reports can be opened in the public repository. Reports
that would expose a vulnerability or personal data should use GitHub's private
security advisory feature instead of a public issue.
