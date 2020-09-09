# cert-manager-community-day

This is the demo code for my talk at the first cert-manager community talks. The slides can be found [here][slides].

The deploy/ folder contains details of how to deploy both cert-manager and manually-managed versions of the webhook.

pkg/admission is a reaaaaally hacky framework for writing validating and mutating webhooks.
main.go contains a simple validating webhook for requiring that pods have resource limits.

[slides]: https://preview.pitch.com/app/presentation/7c93f031-3115-43d0-8aa6-db92147e76ec/9759bebc-3266-43aa-91c2-791c258e2121
