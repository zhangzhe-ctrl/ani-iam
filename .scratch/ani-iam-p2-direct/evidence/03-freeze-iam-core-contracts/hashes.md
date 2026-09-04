# DP2-03 immutable hashes

## Starting identities

| Repository | Branch | Start commit | Result |
| --- | --- | --- | --- |
| ani-iam | `main` | `5ff9f3cfe083b3b911bb076450abbbb967e82a37` | `pass` |
| ANI durable worktree | `codex/direct-p2-01-05` | `a221a7b50c2cfdb13f04c13f154338d836a48af3` | `pass` |

DP2-02 policy revision: `sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa`.

Accepted local artifact commits:

- ani-iam: `1bdc3e3657c233b5a47be706f251a4529ec80b5b`
- ANI: `573d3735934f74f9f1eb78818cddefefd9f575eb`

## Toolchain

| Tool | Version | Executable SHA-256 |
| --- | --- | --- |
| Buf | `1.72.0` | `8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af` |
| protoc-gen-go | `v1.36.12` | `7475078ca943fa552b4755a0b5dd84f4387905a08cb09a47696fd3683cc1c010` |
| protoc-gen-go-grpc | `1.6.2` | `aa1fabbfc27b12d81182864a3f90b47aee907bced808e17e275c5b18c9602b08` |

IAM `buf.lock` pins `buf.build/googleapis/googleapis` commit `c17df5b2beca46928cc87d5656bd5343`, digest `b5:648a01e0170d4512dea7d564016165decd1ed6e34bef79fe54753e51ad7e27545709ad9157d7551270147d551155c595a2fb0bf5bb33b1c83040ddbce915c604`.

## Primary artifacts and pins

| Artifact | SHA-256 |
| --- | --- |
| IAM descriptor | `df863beb3b095d1f01350c5334d80daf10cdf48083ce0e5663781171aa99a001` |
| Core descriptor | `7dd40f9053b7c1c0c8905decab0f81b07173d0b25651113147bde9a5370d352a` |
| IAM/ANI `contract_pins.json` | `33376182b2bcd2f0dd7c84bdf9790d492b6a643560a169e80c0fe63e9113c3b9` |

## IAM source and generation configuration

| File | SHA-256 |
| --- | --- |
| `authentication_service.proto` | `2ddd9e22f794ba127e4ec92b30ebe2ee1a83040edeeb73c241c3835a17f1ed22` |
| `authorization_service.proto` | `7799bef6830dcdd6de8cf19f40b6d4a79361a35c06664e078b8b0ac61cf22050` |
| `contract.proto` | `1ffc52614f8f98d2d99efb30315c2b82c808dd16257c504e8af5995ac06dda0b` |
| `iam_admin_service.proto` | `332dc8ad82bdc9e7c07618808028316a0c9ab735d0b71177095cb7d2f3a7d316` |
| `buf.yaml` | `c58611a82cf9e548dad4f6acc1c3ab8b974e65b635afb7091b64cd3fd3fae6db` |
| `buf.lock` | `dd67624b16fa20c7f3d9cb1ff150a3690425c3de5325f0f6b4bf045df7772db6` |
| `buf.gen.yaml` | `36383d7c9e9e5dfed41358aec66641d4bab4ea6254dea2f9591090a7711b54db` |

## IAM generated Go

| File | SHA-256 |
| --- | --- |
| `authentication_service.pb.go` | `54a41e98cd0cab6aec48f8bbb61c8b5aa8524af89621aa2ff971b19dbbddf8a2` |
| `authentication_service_grpc.pb.go` | `bfa7bf9541f769ec415b8ff0419136999591cfd5707425015ead2ac524b930c5` |
| `authorization_service.pb.go` | `fdd780f5f5a945ff74dc918b3a2a75361e3a05362f4dc412d9facfffe2f02a2e` |
| `authorization_service_grpc.pb.go` | `90bff0d32e6350d980e53884cf7d459a62091d194ee7c91bdbccda2d2be4448a` |
| `contract.pb.go` | `2fc718a4a44165eb8928520348751b3c456f9c28b79c670ffb03753fa5704ae7` |
| `iam_admin_service.pb.go` | `06cf0459771c20f5d0d5cea8c8910a55e5c4fec4f1f14780e9787310a120a196` |
| `iam_admin_service_grpc.pb.go` | `410982325722df2060c105a74a9eee9b2006214f7a1f9ed553af8fdd0148d155` |

## Core source and generated Go

| File | SHA-256 |
| --- | --- |
| `tenant_iam_integration.proto` | `6917e95851c267e0094de9fd9ac30c7b96f7c76984acaee31f2100b244a6732f` |
| task-specific `buf.gen.yaml` | `3968d758fc31fa20b3e15c0c02a453cf71d8892994402b2c03eaf7a29e7f445a` |
| `tenant_iam_integration.pb.go` | `1ea5383bcb217fc86d0db33f39656e952fa42f314e296c59f4d389776c7751db` |
| `tenant_iam_integration_grpc.pb.go` | `3ead1897ebfc8d8b2633ac5420a27ada8205a507e45504730955b116cfdb6f9b` |

## Fixture SHA-256

| Fixture | SHA-256 |
| --- | --- |
| `core_error_contract.v1.json` | `f90d9aed3e369ddd2b310771aee9da7a0fd39f99af55ac514f0a4581b6f94865` |
| `core_tenant_iam_bootstrap_requested.v1.json` | `545017fd5dc4f023bc12fbaca1829dda88924c2872f2cff220424a8c2ad221e8` |
| `core_tenant_lifecycle_changed.v1.json` | `637443dffaf6f60dc9165cae2f2f87b875b90baaa280fa1315e4a3fd5a7436f7` |
| `core_tenant_lifecycle_heartbeat.v1.json` | `05b9715e8262d3a547605bce9312bb6e26b4ccc859ed2e0c18158042395acc00` |
| `core_tenant_lifecycle_snapshot_page.v1.json` | `539a59fcc0691bbdefa13ec5c85081738436b118f6221edd9f026cc816903546` |
| `iam_check_permission.v1.json` | `92c1d517ff3c49532ca08e0f6b24eace2569a8fdb702af8d1d134bc7e846c4c2` |
| `iam_error_contract.v1.json` | `c7a488d5ce5d409d1de25845dd3dc3af7a32c1ce853f81c490d541b8a4448e3c` |
| `iam_password_login.v1.json` | `116532e03642ae266287256f4c1c8f7acd28b24a09851f21ad4cc4fce4d94c67` |

All listed generator outputs were regenerated twice with byte-identical hashes. Result: `pass`.
