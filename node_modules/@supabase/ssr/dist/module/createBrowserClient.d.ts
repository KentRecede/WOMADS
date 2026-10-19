import { SupabaseClient, SupabaseClientOptions } from "@supabase/supabase-js";
import type { CookieMethodsBrowser, CookieMethodsBrowserDeprecated, CookieOptionsWithName } from "./types";
/**
 * Creates a Supabase Client for use in a browser environment.
 *
 * In most cases you should not configure the `options.cookies` object, as this
 * is automatically handled for you. If you do customize this, prefer using the
 * `getAll` and `setAll` functions over `get`, `set` and `remove`. The latter
 * are deprecated due to being difficult to correctly implement and not
 * supporting some edge-cases. Both `getAll` and `setAll` (or both `get`, `set`
 * and `remove`) must be provided. Failing to provide the methods for setting
 * will throw an exception, and in previous versions of the library will result
 * in difficult to debug authentication issues such as random logouts, early
 * session termination or problems with inconsistent state.
 *
 * **The `auth.storage` option is ignored.** The session is always persisted via
 * cookies so that a server-rendered request can read it. Passing
 * `options.auth.storage` has no effect — a one-time console warning is logged
 * if you do. (`options.auth.userStorage` is still respected when `cookies.encode` is `"tokens-only"`.)
 * If you don't need the session to be readable server-side, use
 * `@supabase/supabase-js`'s `createClient` directly with your own `storage`
 * instead; `@supabase/ssr` isn't needed in that case.
 *
 * @param supabaseUrl The URL of the Supabase project.
 * @param supabaseKey The `anon` API key of the Supabase project.
 * @param options Various configuration options.
 *
 * @category Clients
 */
export declare function createBrowserClient<Database = any, SchemaName extends string & keyof Omit<Database, "__InternalSupabase"> = "public" extends keyof Omit<Database, "__InternalSupabase"> ? "public" : string & keyof Omit<Database, "__InternalSupabase">>(supabaseUrl: string, supabaseKey: string, options?: SupabaseClientOptions<SchemaName> & {
    cookies?: CookieMethodsBrowser;
    cookieOptions?: CookieOptionsWithName;
    cookieEncoding?: "raw" | "base64url";
    isSingleton?: boolean;
}): SupabaseClient<Database, SchemaName>;
/**
 * @deprecated Please specify `getAll` and `setAll` cookie methods instead of
 * the `get`, `set` and `remove`. These will not be supported in the next major
 * version.
 */
export declare function createBrowserClient<Database = any, SchemaName extends string & keyof Omit<Database, "__InternalSupabase"> = "public" extends keyof Omit<Database, "__InternalSupabase"> ? "public" : string & keyof Omit<Database, "__InternalSupabase">>(supabaseUrl: string, supabaseKey: string, options?: SupabaseClientOptions<SchemaName> & {
    cookies: CookieMethodsBrowserDeprecated;
    cookieOptions?: CookieOptionsWithName;
    cookieEncoding?: "raw" | "base64url";
    isSingleton?: boolean;
}): SupabaseClient<Database, SchemaName>;
