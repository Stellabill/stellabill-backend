// AUTO-GENERATED FROM openapi/openapi.yaml — DO NOT EDIT.
// Regenerate via: pnpm run generate (sdks/ts)
// Source spec version tracked in openapi.yaml -> info.version

export interface paths {
    "/api/health": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Health check */
        get: operations["getHealth"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/plans": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List billing plans */
        get: operations["listPlans"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/subscriptions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** List subscriptions */
        get: operations["listSubscriptions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/subscriptions/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Get one subscription */
        get: operations["getSubscriptionV1"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/api/v1/idempotency/{key}": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Inspect an idempotency key
         * @description Returns the stored metadata for a previously recorded idempotency key.
         *
         *     Results are **tenant-scoped**: a caller can only inspect keys that were
         *     created under their own tenant/caller identity. Keys that have expired
         *     or were never recorded return `404`.
         *
         */
        get: operations["inspectIdempotencyKey"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        Error: {
            /** @description High-level error type */
            error: string;
            /** @description Human-readable error detail */
            message: string;
            /** @description Machine-readable error code */
            code: string;
        };
        Pagination: {
            /**
             * @description Opaque token to retrieve the next page of results
             * @example Y3Vyc29yX25leHRfcGFnZQ==
             */
            next_cursor?: string;
            /**
             * @description Indicates if there are more results available
             * @example true
             */
            has_more: boolean;
        };
        HealthResponse: {
            /** @example ok */
            status: string;
            /** @example stellarbill-backend */
            service: string;
        };
        Plan: {
            /** @example plan_basic */
            id: string;
            /** @example Basic */
            name: string;
            /** @example 1000 */
            amount: string;
            /** @example NGN */
            currency: string;
            /** @example monthly */
            interval: string;
            /** @example Starter plan */
            description?: string;
        };
        PlansResponse: {
            plans: components["schemas"]["Plan"][];
            pagination: components["schemas"]["Pagination"];
        };
        Subscription: {
            /** @example sub_123 */
            id: string;
            /** @example plan_basic */
            plan_id: string;
            /** @example customer_123 */
            customer: string;
            /** @example active */
            status: string;
            /** @example 1000 */
            amount: string;
            /** @example monthly */
            interval: string;
            /** @example 2026-04-01T00:00:00Z */
            next_billing?: string;
        };
        SubscriptionsResponse: {
            subscriptions: components["schemas"]["Subscription"][];
            pagination: components["schemas"]["Pagination"];
        };
        /** @description Metadata for a stored idempotency key, scoped to the authenticated caller's tenant. */
        IdempotencyKeyRecord: {
            /**
             * @description The idempotency key that was queried.
             * @example order-abc-123
             */
            key: string;
            /**
             * Format: date-time
             * @description When the key was first recorded (RFC 3339 UTC).
             * @example 2026-07-27T10:00:00Z
             */
            used_at: string;
            /**
             * Format: date-time
             * @description When the key will be purged (RFC 3339 UTC).
             * @example 2026-07-28T10:00:00Z
             */
            expires_at: string;
            /**
             * @description HTTP status code stored with the completed response.
             *     `0` indicates the original request is still in-flight.
             *
             * @example 201
             */
            status_code: number;
            /**
             * @description SHA-256 hex digest of the original request body, used to detect key reuse with a different payload.
             * @example e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
             */
            request_fingerprint: string;
        };
    };
    responses: {
        /** @description Validation or client error */
        BadRequest: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["Error"];
            };
        };
        /** @description Missing or invalid authentication */
        Unauthorized: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["Error"];
            };
        };
        /** @description Insufficient permissions */
        Forbidden: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["Error"];
            };
        };
        /** @description Resource not found */
        NotFound: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["Error"];
            };
        };
    };
    parameters: {
        /** @description Opaque cursor for pagination */
        Cursor: string;
        /** @description Maximum number of items to return */
        Limit: number;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    getHealth: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Service is up */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["HealthResponse"];
                };
            };
        };
    };
    listPlans: {
        parameters: {
            query?: {
                /**
                 * @description Pagination cursor for the next page of results
                 * @example Y3Vyc29yX25leHRfcGFnZQ==
                 */
                cursor?: string;
                /**
                 * @description Maximum number of items to return
                 * @example 20
                 */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Plans list */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["PlansResponse"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
        };
    };
    listSubscriptions: {
        parameters: {
            query?: {
                /**
                 * @description Pagination cursor for the next page of results
                 * @example Y3Vyc29yX25leHRfcGFnZQ==
                 */
                cursor?: string;
                /**
                 * @description Maximum number of items to return
                 * @example 20
                 */
                limit?: number;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Subscriptions list */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SubscriptionsResponse"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
        };
    };
    getSubscriptionV1: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /**
                 * @description Subscription identifier
                 * @example sub_123
                 */
                id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Subscription */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Subscription"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
        };
    };
    inspectIdempotencyKey: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /**
                 * @description The idempotency key to inspect.
                 * @example order-abc-123
                 */
                key: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Key found — returns stored metadata. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["IdempotencyKeyRecord"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            404: components["responses"]["NotFound"];
        };
    };
}

