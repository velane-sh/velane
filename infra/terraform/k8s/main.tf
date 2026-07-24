locals {
  namespace = var.create_namespace ? kubernetes_namespace_v1.velane[0].metadata[0].name : var.namespace

  default_labels = {
    "app.kubernetes.io/part-of"    = "velane"
    "app.kubernetes.io/managed-by" = "terraform"
  }

  # When ingress is enabled the services don't need their own load balancers.
  # Fall back to the explicit variable only when ingress is off.
  effective_admin_service_type         = var.enable_ingress ? "ClusterIP" : var.admin_service_type
  effective_control_plane_service_type = var.enable_ingress ? "ClusterIP" : var.control_plane_service_type
  effective_mcp_server_service_type    = var.enable_ingress ? "ClusterIP" : var.mcp_server_service_type

  # With a certificate the ALB listens on 443 and redirects 80→443.
  # Without one it listens on plain 80.
  tls_enabled = var.acm_certificate_arn != ""

  alb_listen_ports = local.tls_enabled ? "[{\"HTTP\":80},{\"HTTPS\":443}]" : "[{\"HTTP\":80}]"
  public_scheme    = local.tls_enabled ? "https" : "http"

  # Nango URLs
  # When we deploy Nango ourselves, we point everything internally at the in-cluster service.
  # Public URLs (for browser / Connect UI) come from var or default to the ingress host + paths.
  effective_nango_internal_url = var.deploy_nango ? "http://nango:3003" : var.nango_internal_url

  effective_nango_connect_url = var.nango_connect_url != "" ? var.nango_connect_url : "${local.public_scheme}://${var.nango_connect_subdomain}.${var.base_domain}"
  effective_nango_api_url     = var.nango_api_url != "" ? var.nango_api_url : "${local.public_scheme}://${var.nango_api_subdomain}.${var.base_domain}"
  effective_mcp_public_url    = var.mcp_public_url != "" ? var.mcp_public_url : (var.enable_ingress ? "${local.public_scheme}://${var.mcp_subdomain}.${var.base_domain}/mcp" : "")
  effective_public_base_url   = var.public_base_url != "" ? var.public_base_url : (var.enable_ingress ? "${local.public_scheme}://${var.admin_subdomain}.${var.base_domain}" : "")

  # Derive a separate Nango database URL from the main Postgres DSN when the user
  # doesn't provide one. Terraform's replace() is plain string replacement, so we
  # split the DSN into path/query pieces instead of trying to regex-rewrite it.
  database_url_parts    = split("?", var.database_url)
  database_url_base     = local.database_url_parts[0]
  database_url_query    = length(local.database_url_parts) > 1 ? "?${join("?", slice(local.database_url_parts, 1, length(local.database_url_parts)))}" : ""
  database_url_segments = split("/", local.database_url_base)
  database_url_prefix   = join("/", slice(local.database_url_segments, 0, length(local.database_url_segments) - 1))

  effective_nango_database_url = var.nango_database_url != "" ? var.nango_database_url : (
    var.create_nango_database ?
    "${local.database_url_prefix}/${var.nango_db_name}${local.database_url_query}" :
    var.database_url
  )
}

provider "kubernetes" {
  config_path    = var.kubeconfig_path
  config_context = var.kubeconfig_context != "" ? var.kubeconfig_context : null
}

resource "kubernetes_namespace_v1" "velane" {
  count = var.create_namespace ? 1 : 0

  metadata {
    name   = var.namespace
    labels = local.default_labels
  }
}

resource "kubernetes_secret_v1" "control_plane" {
  metadata {
    name      = "control-plane-secrets"
    namespace = local.namespace
    labels    = local.default_labels
  }

  type = "Opaque"

  data = {
    DATABASE_URL                           = var.database_url
    REDIS_URL                              = var.redis_url
    ENCRYPTION_KEY                         = var.encryption_key
    JWT_PRIVATE_KEY                        = var.jwt_private_key_pem
    BOOTSTRAP_EMAIL                        = var.bootstrap_email
    BOOTSTRAP_PASSWORD                     = var.bootstrap_password
    BOOTSTRAP_TENANT                       = var.bootstrap_tenant
    NANGO_SECRET_KEY                       = var.nango_secret_key
    NANGO_PUBLIC_KEY                       = var.nango_public_key
    NANGO_WEBHOOK_SECRET                   = var.nango_webhook_secret
    GOOGLE_OAUTH_CLIENT_ID                 = var.google_oauth_client_id
    GOOGLE_OAUTH_CLIENT_SECRET             = var.google_oauth_client_secret
    GITHUB_OAUTH_CLIENT_ID                 = var.github_oauth_client_id
    GITHUB_OAUTH_CLIENT_SECRET             = var.github_oauth_client_secret
    OBJECT_STORAGE_AZURE_CONNECTION_STRING = var.object_storage_azure_connection_string
    AWS_ACCESS_KEY_ID                      = var.object_storage_access_key_id
    AWS_SECRET_ACCESS_KEY                  = var.object_storage_secret_access_key
  }
}

resource "kubernetes_service_account_v1" "control_plane" {
  metadata {
    name        = "control-plane"
    namespace   = local.namespace
    labels      = local.default_labels
    annotations = var.control_plane_service_account_annotations
  }
}

resource "kubernetes_deployment_v1" "bun_executor" {
  metadata {
    name      = "bun-executor"
    namespace = local.namespace
    labels = merge(local.default_labels, {
      "app.kubernetes.io/name" = "bun-executor"
    })
  }

  spec {
    replicas = var.bun_executor_replicas

    selector {
      match_labels = {
        app = "bun-executor"
      }
    }

    template {
      metadata {
        labels = {
          app = "bun-executor"
        }
      }

      spec {
        container {
          name              = "bun-executor"
          image             = var.bun_executor_image
          image_pull_policy = var.image_pull_policy

          port {
            container_port = 8080
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "bun_executor" {
  metadata {
    name      = "bun-executor"
    namespace = local.namespace
    labels    = local.default_labels
  }

  spec {
    selector = {
      app = "bun-executor"
    }

    port {
      name        = "http"
      port        = 8080
      target_port = 8080
    }

    type = "ClusterIP"
  }
}

resource "kubernetes_deployment_v1" "python_executor" {
  metadata {
    name      = "python-executor"
    namespace = local.namespace
    labels = merge(local.default_labels, {
      "app.kubernetes.io/name" = "python-executor"
    })
  }

  spec {
    replicas = var.python_executor_replicas

    selector {
      match_labels = {
        app = "python-executor"
      }
    }

    template {
      metadata {
        labels = {
          app = "python-executor"
        }
      }

      spec {
        container {
          name              = "python-executor"
          image             = var.python_executor_image
          image_pull_policy = var.image_pull_policy

          port {
            container_port = 8080
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "python_executor" {
  metadata {
    name      = "python-executor"
    namespace = local.namespace
    labels    = local.default_labels
  }

  spec {
    selector = {
      app = "python-executor"
    }

    port {
      name        = "http"
      port        = 8080
      target_port = 8080
    }

    type = "ClusterIP"
  }
}

resource "kubernetes_deployment_v1" "control_plane" {
  metadata {
    name      = "control-plane"
    namespace = local.namespace
    labels = merge(local.default_labels, {
      "app.kubernetes.io/name" = "control-plane"
    })
  }

  spec {
    replicas = var.control_plane_replicas

    selector {
      match_labels = {
        app = "control-plane"
      }
    }

    template {
      metadata {
        labels = {
          app = "control-plane"
        }
      }

      spec {
        service_account_name = kubernetes_service_account_v1.control_plane.metadata[0].name

        container {
          name              = "control-plane"
          image             = var.control_plane_image
          image_pull_policy = var.image_pull_policy

          port {
            container_port = 8080
          }

          env {
            name  = "PORT"
            value = "8080"
          }

          env {
            name  = "BUN_EXECUTOR_URL"
            value = "http://bun-executor:8080"
          }

          env {
            name  = "PYTHON_EXECUTOR_URL"
            value = "http://python-executor:8080"
          }

          env {
            name  = "WORKER_COUNT"
            value = tostring(var.worker_count)
          }

          env {
            name  = "EXECUTOR_TYPE"
            value = var.executor_type
          }

          env {
            name  = "INTERNAL_PROXY_URL"
            value = "http://control-plane:8080"
          }

          env {
            name  = "CLICKHOUSE_DSN"
            value = var.clickhouse_dsn
          }

          env {
            name  = "LOGS_BUCKET"
            value = var.logs_bucket
          }

          env {
            name  = "REPLAY_BUCKET"
            value = var.replay_bucket
          }

          env {
            name  = "OBJECT_STORAGE_DRIVER"
            value = var.object_storage_driver
          }

          env {
            name  = "OBJECT_STORAGE_BUCKET"
            value = var.object_storage_bucket
          }

          env {
            name  = "OBJECT_STORAGE_PREFIX"
            value = var.object_storage_prefix
          }

          env {
            name  = "OBJECT_STORAGE_S3_REGION"
            value = var.object_storage_s3_region
          }

          env {
            name  = "OBJECT_STORAGE_S3_ENDPOINT"
            value = var.object_storage_s3_endpoint
          }

          env {
            name  = "OBJECT_STORAGE_S3_FORCE_PATH_STYLE"
            value = tostring(var.object_storage_s3_force_path_style)
          }

          env {
            name  = "OBJECT_STORAGE_AZURE_ACCOUNT_URL"
            value = var.object_storage_azure_account_url
          }

          env {
            name  = "OBJECT_GC_GRACE_PERIOD"
            value = var.object_gc_grace_period
          }

          env {
            name  = "INVOCATION_RETENTION"
            value = var.invocation_retention
          }

          env {
            name = "DATABASE_URL"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "DATABASE_URL"
              }
            }
          }

          env {
            name = "REDIS_URL"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "REDIS_URL"
              }
            }
          }

          env {
            name = "OBJECT_STORAGE_AZURE_CONNECTION_STRING"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "OBJECT_STORAGE_AZURE_CONNECTION_STRING"
              }
            }
          }

          env {
            name = "AWS_ACCESS_KEY_ID"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "AWS_ACCESS_KEY_ID"
              }
            }
          }

          env {
            name = "AWS_SECRET_ACCESS_KEY"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "AWS_SECRET_ACCESS_KEY"
              }
            }
          }

          env {
            name = "ENCRYPTION_KEY"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "ENCRYPTION_KEY"
              }
            }
          }

          env {
            name = "JWT_PRIVATE_KEY"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "JWT_PRIVATE_KEY"
              }
            }
          }

          env {
            name = "BOOTSTRAP_EMAIL"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "BOOTSTRAP_EMAIL"
              }
            }
          }

          env {
            name = "BOOTSTRAP_PASSWORD"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "BOOTSTRAP_PASSWORD"
              }
            }
          }

          env {
            name = "BOOTSTRAP_TENANT"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "BOOTSTRAP_TENANT"
              }
            }
          }

          env {
            name = "NANGO_SECRET_KEY"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "NANGO_SECRET_KEY"
              }
            }
          }

          env {
            name = "NANGO_PUBLIC_KEY"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "NANGO_PUBLIC_KEY"
              }
            }
          }

          env {
            name = "NANGO_WEBHOOK_SECRET"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "NANGO_WEBHOOK_SECRET"
              }
            }
          }

          # Point control-plane at Nango (either the one we deploy or external)
          env {
            name  = "NANGO_INTERNAL_URL"
            value = local.effective_nango_internal_url
          }
          env {
            name  = "NANGO_CONNECT_URL"
            value = local.effective_nango_connect_url
          }
          env {
            name  = "NANGO_API_URL"
            value = local.effective_nango_api_url
          }
          env {
            name  = "MCP_PUBLIC_URL"
            value = local.effective_mcp_public_url
          }
          env {
            name  = "PUBLIC_BASE_URL"
            value = local.effective_public_base_url
          }
          env {
            name  = "VELANE_CLOUD"
            value = var.velane_cloud ? "true" : "false"
          }
          env {
            name = "GOOGLE_OAUTH_CLIENT_ID"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "GOOGLE_OAUTH_CLIENT_ID"
              }
            }
          }
          env {
            name = "GOOGLE_OAUTH_CLIENT_SECRET"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "GOOGLE_OAUTH_CLIENT_SECRET"
              }
            }
          }
          env {
            name = "GITHUB_OAUTH_CLIENT_ID"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "GITHUB_OAUTH_CLIENT_ID"
              }
            }
          }
          env {
            name = "GITHUB_OAUTH_CLIENT_SECRET"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.control_plane.metadata[0].name
                key  = "GITHUB_OAUTH_CLIENT_SECRET"
              }
            }
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "control_plane" {
  metadata {
    name      = "control-plane"
    namespace = local.namespace
    labels    = local.default_labels
  }

  spec {
    selector = {
      app = "control-plane"
    }

    port {
      name        = "http"
      port        = 8080
      target_port = 8080
    }

    type = local.effective_control_plane_service_type
  }
}

resource "kubernetes_deployment_v1" "mcp_server" {
  metadata {
    name      = "mcp-server"
    namespace = local.namespace
    labels = merge(local.default_labels, {
      "app.kubernetes.io/name" = "mcp-server"
    })
  }

  spec {
    replicas = var.mcp_server_replicas

    selector {
      match_labels = {
        app = "mcp-server"
      }
    }

    template {
      metadata {
        labels = {
          app = "mcp-server"
        }
      }

      spec {
        container {
          name              = "mcp-server"
          image             = var.mcp_server_image
          image_pull_policy = var.image_pull_policy

          port {
            container_port = 8090
          }

          env {
            name  = "PORT"
            value = "8090"
          }

          env {
            name  = "CONTROL_PLANE_URL"
            value = "http://control-plane:8080"
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "mcp_server" {
  metadata {
    name      = "mcp-server"
    namespace = local.namespace
    labels    = local.default_labels
  }

  spec {
    selector = {
      app = "mcp-server"
    }

    port {
      name        = "http"
      port        = 8090
      target_port = 8090
    }

    type = local.effective_mcp_server_service_type
  }
}

resource "kubernetes_deployment_v1" "admin" {
  metadata {
    name      = "admin"
    namespace = local.namespace
    labels = merge(local.default_labels, {
      "app.kubernetes.io/name" = "admin"
    })
  }

  spec {
    replicas = var.admin_replicas

    selector {
      match_labels = {
        app = "admin"
      }
    }

    template {
      metadata {
        labels = {
          app = "admin"
        }
      }

      spec {
        container {
          name              = "admin"
          image             = var.admin_image
          image_pull_policy = var.image_pull_policy

          port {
            container_port = 80
          }

          port {
            container_port = 3003
          }

          port {
            container_port = 3009
          }
        }
      }
    }
  }
}

resource "kubernetes_service_v1" "admin" {
  metadata {
    name      = "admin"
    namespace = local.namespace
    labels    = local.default_labels
  }

  spec {
    selector = {
      app = "admin"
    }

    port {
      name        = "http"
      port        = 80
      target_port = 80
    }

    port {
      name        = "nango-api"
      port        = 3003
      target_port = 3003
    }

    port {
      name        = "nango-connect"
      port        = 3009
      target_port = 3009
    }

    type = local.effective_admin_service_type
  }
}

# =============================================================================
# NANGO (in-cluster deployment)
# =============================================================================

resource "kubernetes_secret_v1" "nango" {
  count = var.deploy_nango ? 1 : 0

  metadata {
    name      = "nango-secrets"
    namespace = local.namespace
    labels    = local.default_labels
  }

  type = "Opaque"

  data = {
    NANGO_DATABASE_URL   = local.effective_nango_database_url
    NANGO_ENCRYPTION_KEY = var.nango_encryption_key
    NANGO_SECRET_KEY_DEV = var.nango_secret_key != "" ? var.nango_secret_key : "d4aff6fd-3031-4dc5-8027-c067a50c6c5a"
    NANGO_SECRET_KEY     = var.nango_secret_key
    NANGO_PUBLIC_KEY     = var.nango_public_key
    NANGO_WEBHOOK_SECRET = var.nango_webhook_secret
  }
}

# One-shot job that creates the dedicated "nango" database on the shared Postgres
# when the user did not supply an explicit nango_database_url.
resource "kubernetes_job_v1" "create_nango_database" {
  count = var.deploy_nango && var.create_nango_database && var.nango_database_url == "" ? 1 : 0

  metadata {
    name      = "create-nango-database"
    namespace = local.namespace
    labels    = local.default_labels
  }

  spec {
    template {
      metadata {
        labels = local.default_labels
      }

      spec {
        restart_policy = "OnFailure"

        container {
          name  = "create-db"
          image = "postgres:16-alpine"

          env {
            name  = "DATABASE_URL"
            value = var.database_url
          }

          command = [
            "sh", "-c",
            <<EOT
              echo "Ensuring database '${var.nango_db_name}' exists..."
              psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'CREATE DATABASE ${var.nango_db_name}'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${var.nango_db_name}')\gexec
SQL
              echo "Done."
            EOT
          ]
        }
      }
    }

    backoff_limit = 4
  }
}

resource "kubernetes_deployment_v1" "nango" {
  count = var.deploy_nango ? 1 : 0

  metadata {
    name      = "nango"
    namespace = local.namespace
    labels = merge(local.default_labels, {
      "app.kubernetes.io/name" = "nango"
    })
  }

  spec {
    replicas = var.nango_replicas

    selector {
      match_labels = {
        app = "nango"
      }
    }

    template {
      metadata {
        labels = {
          app = "nango"
        }
      }

      spec {
        container {
          name              = "nango"
          image             = var.nango_image
          image_pull_policy = var.image_pull_policy

          port {
            container_port = 3003
            name           = "api"
          }

          port {
            container_port = 3009
            name           = "connect"
          }

          env {
            name = "NANGO_DATABASE_URL"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.nango[0].metadata[0].name
                key  = "NANGO_DATABASE_URL"
              }
            }
          }

          env {
            name = "NANGO_ENCRYPTION_KEY"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.nango[0].metadata[0].name
                key  = "NANGO_ENCRYPTION_KEY"
              }
            }
          }

          env {
            name = "NANGO_SECRET_KEY_DEV"
            value_from {
              secret_key_ref {
                name = kubernetes_secret_v1.nango[0].metadata[0].name
                key  = "NANGO_SECRET_KEY_DEV"
              }
            }
          }

          env {
            name  = "SERVER_PORT"
            value = "3003"
          }

          env {
            name  = "NANGO_PORT"
            value = "3003"
          }

          env {
            name  = "NANGO_SERVER_URL"
            value = local.effective_nango_api_url
          }

          env {
            name  = "NANGO_PUBLIC_CONNECT_URL"
            value = local.effective_nango_connect_url
          }

          env {
            name  = "FLAG_SERVE_CONNECT_UI"
            value = "true"
          }

          env {
            name  = "FLAG_API_RATE_LIMIT_ENABLED"
            value = "false"
          }

          env {
            name  = "NANGO_CONNECT_UI_PORT"
            value = "3009"
          }

          env {
            name  = "TELEMETRY"
            value = "false"
          }
        }
      }
    }
  }

  depends_on = [kubernetes_job_v1.create_nango_database]
}

resource "kubernetes_service_v1" "nango" {
  count = var.deploy_nango ? 1 : 0

  metadata {
    name      = "nango"
    namespace = local.namespace
    labels    = local.default_labels
  }

  spec {
    selector = {
      app = "nango"
    }

    port {
      name        = "api"
      port        = 3003
      target_port = 3003
    }

    port {
      name        = "connect"
      port        = 3009
      target_port = 3009
    }

    type = "ClusterIP"
  }
}

# =============================================================================
# INGRESS (path-based, ALB / nginx / cloud equivalent)
# =============================================================================

resource "kubernetes_ingress_v1" "velane" {
  count = var.enable_ingress ? 1 : 0

  metadata {
    name      = "velane"
    namespace = local.namespace
    labels    = local.default_labels

    annotations = merge(
      {
        "kubernetes.io/ingress.class" = var.ingress_class_name
      },
      var.ingress_class_name == "alb" ? merge(
        {
          "alb.ingress.kubernetes.io/scheme"       = "internet-facing"
          "alb.ingress.kubernetes.io/target-type"  = "ip"
          "alb.ingress.kubernetes.io/listen-ports" = local.alb_listen_ports
        },
        local.tls_enabled ? {
          "alb.ingress.kubernetes.io/certificate-arn" = var.acm_certificate_arn
          "alb.ingress.kubernetes.io/ssl-redirect"    = "443"
        } : {}
      ) : {},
      var.ingress_annotations
    )
  }

  spec {
    ingress_class_name = var.ingress_class_name

    rule {
      host = "${var.admin_subdomain}.${var.base_domain}"
      http {
        path {
          path      = "/"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.admin.metadata[0].name
              port { number = 80 }
            }
          }
        }
      }
    }

    rule {
      host = "${var.api_subdomain}.${var.base_domain}"
      http {
        path {
          path      = "/"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.control_plane.metadata[0].name
              port { number = 8080 }
            }
          }
        }
      }
    }

    rule {
      host = "${var.mcp_subdomain}.${var.base_domain}"
      http {
        path {
          path      = "/"
          path_type = "Prefix"
          backend {
            service {
              name = kubernetes_service_v1.mcp_server.metadata[0].name
              port { number = 8090 }
            }
          }
        }
      }
    }

    dynamic "rule" {
      for_each = var.deploy_nango ? [1] : []
      content {
        host = "${var.nango_connect_subdomain}.${var.base_domain}"
        http {
          path {
            path      = "/"
            path_type = "Prefix"
            backend {
              service {
                name = kubernetes_service_v1.nango[0].metadata[0].name
                port { number = 3009 }
              }
            }
          }
        }
      }
    }

    dynamic "rule" {
      for_each = var.deploy_nango ? [1] : []
      content {
        host = "${var.nango_api_subdomain}.${var.base_domain}"
        http {
          path {
            path      = "/"
            path_type = "Prefix"
            backend {
              service {
                name = kubernetes_service_v1.nango[0].metadata[0].name
                port { number = 3003 }
              }
            }
          }
        }
      }
    }
  }
}
