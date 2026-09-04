# DP2-02 immutable inputs and artifacts

Result: `pass`

## Fixed inputs

| Artifact | Identity |
| --- | --- |
| ANI source commit | `0cedae825a489d936cf41815dc27f278f6d3213c` |
| ANI source tree | `552e50bd5bdd49b6abb168b1ef99bfb74dbd1df8` |
| Fixed Core OpenAPI | `b6a2dc1f9c596555fcc164240d7a5533a04a128b8e19a4d7af1c53e41ab9415b` |
| Fixed Services OpenAPI | `43d91489cb851c3e1ac1ffef2ec167391ae210e2da7d794bcb22ffe2d057aad6` |
| Accepted DP2-01 IAM commit | `da3a7af2fb1ffa1d86f167666cccc951f2ddbdcd` |
| Accepted deletion manifest | `f68d16af8bc9fa5540e5c5de538997b746f2368029e070d11aff98a6776bd088` |
| Accepted ANI DP2-02 local commit | `a221a7b50c2cfdb13f04c13f154338d836a48af3` |
| Baseline Gateway registry generator | `b1c6ca039de158fa36f7aaa644e5c3b5df31c87ffaf49db159e779aaa19cf006` |
| Baseline SDK generator | `f9d35b5f8ac60bcbcf04a5640e379b96ea1bb24915cd2869e50c9b9fe603b056` |

## Target artifacts

| Artifact | SHA-256 |
| --- | --- |
| `repo/api/openapi/v1.yaml` | `2466982a7e8f904c6bb6f7790588359c6faf9b230a39a0e28939fcbedc72d0e5` |
| `repo/api/openapi/operation-policy.v1.yaml` | `851d236c93b80af097d3b319ee064615d553de64b53128892f39c3c8fda282a1` |
| `repo/api/openapi/iam-replacement.v1.yaml` | `dffb3c5b2849cfa60a8d121976945420b5da794c14543ceb8c28f5ba9e19e780` |
| `repo/api/openapi/operation-registry.v1.json` | `319bd3746098b79d29da18141872b97263c1d899da0306654eda9fad736c2ad2` |
| `repo/api/openapi/openapi-breaking.v1.json` | `cc45d4116b99a525ed58c29a1e33daae194c3cd8f54a6e36c9fdf9859e01be9a` |
| target registry generator | `2f5824d58bfd2e5b9e401d9dc8d3a749edcc8170f15ea6cf202ce228d0c7ae51` |
| breaking-report generator | `de403871d8455c4be28b58740ed00e35b804ae39781b08fefcbd85b265d635a6` |
| generated target Go registry | `b28b90fef06dc013c61c4d339574262e27505de5d5a207a2d9fd59b6ed7f0bdc` |
| Console generated Core schema | `d0e522b59245bb0a236ed17a9771fef8b56518f24165a83c0564cc51cbe14459` |
| BOSS generated Core schema | `d0e522b59245bb0a236ed17a9771fef8b56518f24165a83c0564cc51cbe14459` |
| Core SDK Go client | `2d07cd17b16ad92c728e18b5c5b13431187db904d714fe8d6da967d0c59a1e69` |
| Core SDK Java client | `ba0082b6c42bb07f8002e56ddb6a1744f14361dd86f2905ea7ab6438bb50d4f7` |
| Core SDK Python client | `e8e3664560183a3fc96283d5f294d00bcb79b54cf97fc86b7f9d0b46e9289457` |
| Core SDK metadata | `349422566a1c24c6ade9dd42930ba9002f8ce714a761968224d14d089d8da74c` |
| Core SDK TypeScript runtime | `9def646e21d2acf2c39f7d42af520d25346a068b9142706a5b654e81e91ad596` |
| Core SDK TypeScript declarations | `663eca4860fb371c6d11ab368a794dc39f745dd9725e85253317ccd4d1ff43d6` |

Policy revision: `sha256:f222e2c6d3cd6442449cd722389d3d4fbfcdc7a0fee950c9d28385d3c264affa`.

Toolchain: Go `go1.26.7-X:nodwarf5 linux/amd64`; Python `3.14.7`; PyYAML `6.0.3`; openapi-typescript `7.13.0`.

Deterministic `--check`, fresh TypeScript comparison and isolated SDK smoke results are recorded in `commands.md`.
