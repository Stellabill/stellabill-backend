package admin.authz

default allow = false

allow {
    input.token == "secret-admin-token"
}
