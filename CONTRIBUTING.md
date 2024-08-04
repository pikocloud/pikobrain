## Local development

Requirements:

- [golang](https://go.dev/dl/) 1.22.5+
- [goreleaser](https://goreleaser.com/install)
- [docker](https://docs.docker.com/engine/install/ubuntu/) - please follow guides for your OS distribution
- Linux (preffered) or MacOSX

> tip: you may install [direnv](https://direnv.net) and put test tokens in .env file - the file is .gitignore-d

## Local build


    goreleaser release --snapshot --clean