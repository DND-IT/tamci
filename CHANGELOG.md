# Changelog

## [0.2.5](https://github.com/DND-IT/tamci/compare/v0.2.4...v0.2.5) (2026-09-24)


### Features

* **image:** publish multi-arch image for amd64 and arm64 ([#18](https://github.com/DND-IT/tamci/issues/18)) ([9e51d8b](https://github.com/DND-IT/tamci/commit/9e51d8b82e540b0ad0857dd31691463fc5a3b784))

## [0.2.4](https://github.com/DND-IT/tamci/compare/v0.2.3...v0.2.4) (2026-09-23)


### Features

* **rollout:** let a service list the environments it rolls out to ([#16](https://github.com/DND-IT/tamci/issues/16)) ([3581fe0](https://github.com/DND-IT/tamci/commit/3581fe0d4869c0d12a7533967d935e16ee210a5e))

## [0.2.3](https://github.com/DND-IT/tamci/compare/v0.2.2...v0.2.3) (2026-09-23)


### Features

* **release:** scope bumps to include-path and improve release notes ([#12](https://github.com/DND-IT/tamci/issues/12)) ([4ef9a68](https://github.com/DND-IT/tamci/commit/4ef9a68a865891d6a388a71a4d359f9bccbf0069))
* **sts-lint:** add sts-lint subcommand to check octo-sts trust policies ([#14](https://github.com/DND-IT/tamci/issues/14)) ([f698f81](https://github.com/DND-IT/tamci/commit/f698f81844c5afb5c9727d8b3901b12c934f9df3))
* **token:** add token subcommand for octo-sts brokers ([#10](https://github.com/DND-IT/tamci/issues/10)) ([14f41b0](https://github.com/DND-IT/tamci/commit/14f41b0e7c1b4b211500c29780af4ec9c28c5716))


### Bug Fixes

* **rollout:** name direct-mode PRs by service and environment ([#11](https://github.com/DND-IT/tamci/issues/11)) ([848bdb2](https://github.com/DND-IT/tamci/commit/848bdb2a43aa28c5cf2a531348db4a37570dbc12))

## [0.2.2](https://github.com/DND-IT/tamci/compare/v0.2.1...v0.2.2) (2026-09-08)


### Bug Fixes

* **release:** release PR merge fixes for path-filtered pipelines and dry-run ([#8](https://github.com/DND-IT/tamci/issues/8)) ([d9b0d91](https://github.com/DND-IT/tamci/commit/d9b0d916ff367eb5efefc3a6f1365dbd4b77c063))

## [0.2.1](https://github.com/DND-IT/tamci/compare/v0.2.0...v0.2.1) (2026-09-08)


### Bug Fixes

* **release:** keep runner-provided inputs over flag defaults ([#6](https://github.com/DND-IT/tamci/issues/6)) ([ce9062e](https://github.com/DND-IT/tamci/commit/ce9062e60e1dd3c991bfbbc64da8225faa5b523c))

## [0.2.0](https://github.com/DND-IT/tamci/compare/v0.1.1...v0.2.0) (2026-09-08)


### ⚠ BREAKING CHANGES

* calver versions are YYYY.MM.N instead of YYYY.MM.DD[.N]. Repos already on calver will see existing day-based tags reinterpreted as counters for the transition month.

### Features

* release 0.2.0 with in-repo actions, calver YYYY.MM.N, and test coverage ([#4](https://github.com/DND-IT/tamci/issues/4)) ([cc6154b](https://github.com/DND-IT/tamci/commit/cc6154befcf0e34e283026e47112e49799aeb573))

## [0.1.1](https://github.com/DND-IT/tamci/compare/v0.1.0...v0.1.1) (2026-06-11)


### Bug Fixes

* **release:** port action-releaser v0.3.x bug fixes ([80b4d69](https://github.com/DND-IT/tamci/commit/80b4d696e3a8ba6d4936dee35f74ef9c65b1029a))
* **release:** port action-releaser v0.3.x bug fixes ([9f124fd](https://github.com/DND-IT/tamci/commit/9f124fd2cee1b06adf60b757e7e23ccd7e36b221))
* resolve golangci-lint errcheck and staticcheck findings ([7e29feb](https://github.com/DND-IT/tamci/commit/7e29feb981465dbf0d2ac3e5df73f2f727bbe6e9))
