FROM golang:1.15 as builder

WORKDIR $GOPATH/src/github.com/endocrimes/cert-manager-community-day

COPY . .

RUN CGO_ENABLED=0 go build -o /admission-webhook

FROM scratch

COPY --from=builder /admission-webhook /admission-webhook
ENTRYPOINT [ "/admission-webhook" ]
