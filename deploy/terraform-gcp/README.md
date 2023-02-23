# terraform-gcp-relayer

This repo contains a Terraform plan for deploying `relayer` on GCP

## What are Nostr Relays?
Relays are like the backend servers for Nostr. They allow Nostr clients to send them messages, and they may (or may not) store those messages and broadcast those messages to all other connected clients. The world of relays is changing fast so expect many changes here in the future.

## Install requirements

### gcloud CLI

In order for Terraform to run operations on your behalf, you must install and configure the gcloud CLI tool. To install the gcloud CLI, follow the [installation guide](https://cloud.google.com/sdk/docs/install) for your system.

After the installation perform the steps outlined below. This will authorize the SDK to access GCP using your user account credentials and add the SDK to your PATH. It requires you to login and select the project you want to work in. Then add your account to the Application Default Credentials (ADC). This will allow Terraform to access these credentials to provision resources on GCP.

```bash
gcloud auth application-default login
```

## Requirements

| Name | Version |
| ---- | ------- |
| terraform | >=1.3.2 |
| gcp | >=4.47.0 |

## Providers

|Name | Version |
| --- | ------- |
| gcp | >=4.47.0 |

## Terraform Resources

| Name | Type |
| ---------| ------------|
| `google_service_account` | Resource |
| `google_project_iam_member` | Resource |
| `google_compute_firewall` | Resource |
| `google_compute_address` | Resource |
| `google_compute_instance` | Resource |

## Inputs

| Name |  Type | Required|
| ---- |  ---- | ------- |
| `project_id` |  string | yes
| `machine_type` | string | yes |
| `region` | string | yes |
| `public_keypath` |  string | yes |
| `private_keypath` | string | yes |
