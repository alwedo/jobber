# jobber

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/alwedo/jobber) ![Test](https://github.com/alwedo/jobber/actions/workflows/test.yml/badge.svg)

Jobber is a dynamic job search RSS feed generator. 

Are you tired of going from job portal to job portal doing your search? Those days are over! Jobber allows you to create job searches that will update hourly in the background and provide you an RSS feed for them.

Check it out! [rssjobs.app](https://rssjobs.app/)

## Features

- Currently scraping LinkedIn<sup>*</sup>.
- Initial job searches will return up to 7 days of offers.
- RSS Feed will display up to 7 days of offers.
- Job searches that are not used for 7 days will be automatically deleted (ie. unsubscribed from the RSS feed).
- Server usage and status metrics with Prometheus and Grafana.

<sup>*</sup> _jobber scrapes only publicly available information_

## Starting up the project locally

Make sure you have [go](https://go.dev/doc/install), [Docker](https://docs.docker.com/engine/install/) and [golang-migrate](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate) installed.

- (optional) Run test and lint with `make check`

### Production mode

This will build the db, the server and all the M&O infrastructure in Docker.

- Start production mode with `make build`

Once up, try `http://localhost` for your local version of jobber, or go to the Grafana dashboard with `http://localhost:3000/dashboards`.

### Developer mode

This will create only the DB container and run the server without building it.

- Start developer mode with `make run`

Once up, try `http://localhost` for your local dev version of jobber.
