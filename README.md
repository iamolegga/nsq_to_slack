<h1 align="center">nsq_to_slack</h1>
<p align="center">Forward NSQ messages to slack</p>

<p align="center">
  <a href="https://hub.docker.com/r/iamolegga/nsq_to_slack">
    <img alt="Docker Image Version (latest semver)" src="https://img.shields.io/docker/v/iamolegga/nsq_to_slack?sort=semver">
  </a>
  <a href="https://github.com/iamolegga/nsq_to_slack/actions/workflows/on-push-main.yml?query=branch%3Amain">
    <img alt="GitHub Workflow Status (with branch)" src="https://img.shields.io/github/actions/workflow/status/iamolegga/nsq_to_slack/on-push-main.yml?branch=main">
  </a>
  <a href="https://libraries.io/github/iamolegga/nsq_to_slack">
    <img alt="Libraries.io dependency status for GitHub repo" src="https://img.shields.io/librariesio/github/iamolegga/nsq_to_slack" />
  </a>
  <img alt="Dependabot" src="https://badgen.net/github/dependabot/iamolegga/nsq_to_slack" />
  <img alt="Docker Pulls" src="https://img.shields.io/docker/pulls/iamolegga/nsq_to_slack" />
</p>

## Usage

```
$ ./nsq_to_slack --help
Usage of ./nsq_to_slack:
  -channel value
    	NSQ channel (may be given multiple times)
  -gom
    	Expose Go runtime metrics
  -log value
    	log level (debug, info, warn, error, dpanic, panic, fatal)
  -lookupd-http-address string
    	nsqlookupd HTTP address
  -nsqd-tcp-address string
    	nsqd TCP address (default "localhost:4150")
  -slack-webhook-url string
    	Slack webhook URL
  -topic value
    	NSQ topic (may be given multiple times)
```

Or in docker:

```shell
docker run --rm -p 9090:9090 iamolegga/nsq_to_slack \
  -nsqd-tcp-address=host.docker.internal:4150 \
  -topic foo -channel bar \
  -topic baz -channel qux \
  -slack-webhook-url=https://hooks.slack.com/services/...
```

Metrics are exposed at `/metrics` endpoint on :9090 port.

## Screenshot

![screenshot](./img.png)
