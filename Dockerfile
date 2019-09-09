# syntax=docker/dockerfile:experimental
FROM golang:1.13-alpine3.10 as build
WORKDIR /src
RUN apk add --no-cache --update openssh-client git curl build-base ca-certificates
RUN git config --system url."ssh://git@github.com/".insteadOf "https://github.com/"
RUN mkdir -p -m 0600 ~/.ssh && ssh-keyscan -t rsa github.com >> ~/.ssh/known_hosts
RUN set -o pipefail && ssh-keygen -F github.com -l -E sha256 | grep -q "SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw6E5SY8"
ENV PATH "/go/bin/:$PATH"
COPY ./go.mod ./go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build mkdir -p /var/ssh && GIT_SSH_COMMAND="ssh -o \"ControlMaster auto\" -o \"ControlPersist 300\" -o \"ControlPath /var/ssh/%r@%h:%p\"" go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
        GOOS=linux GOARCH=amd64 go install \
        -ldflags "-w -s -extldflags '-static' -X main.Version=$VERSION -X 'main.BuildTime=$BUILD_TIME'" \
        github.com/ngerakines/gcp-budget-notifier/...

FROM alpine:latest
RUN apk add --no-cache --update ca-certificates
RUN mkdir -p /app
WORKDIR /app
COPY --from=build /go/bin/gcp-budget-notifier /go/bin/
STOPSIGNAL SIGINT
ENTRYPOINT ["/go/bin/gcp-budget-notifier"]
