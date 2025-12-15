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

## Setting up the project locally

Make sure you have [go](https://go.dev/doc/install), [Docker](https://docs.docker.com/engine/install/) and [golang-migrate](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate) installed.

- (optional) Run test and lint with `make check`
- Build the server with `make init`

Once up, try `http://localhost:80` for your local version of jobber, or go to the Grafana dashboard with `http://localhost:3000/dashboards`.
