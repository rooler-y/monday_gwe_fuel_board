FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /out/fuelboard ./cmd/fuelboard

FROM alpine:3.20
RUN apk add --no-cache ca-certificates util-linux
WORKDIR /app
COPY --from=build /out/fuelboard ./bin/fuelboard
COPY migrations ./migrations
COPY scripts ./scripts
RUN chmod +x ./scripts/*.sh

# entrypoint.sh applies pending migrations, then execs CMD. Default CMD runs
# the 15-min collect+publish cycle; override to ./scripts/daily_fuel_report.sh
# for the once-a-day MPG pull, or to ./bin/fuelboard <subcommand> to run a
# single step directly.
ENTRYPOINT ["./scripts/entrypoint.sh"]
CMD ["./scripts/frequent_cycle.sh"]
