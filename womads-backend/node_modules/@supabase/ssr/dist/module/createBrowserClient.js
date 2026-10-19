import { createClient, } from "@supabase/supabase-js";
import { createStorageFromOptions } from "./cookies";
import { isBrowser, memoryLocalStorageAdapter } from "./utils";
import { VERSION } from "./version";
import { warnOnce } from "./warnOnce";
import { warnIfUsingDeprecatedAuthHelpersPackage } from "./warnDeprecatedPackage";
let cachedBrowserClient;
export function createBrowserClient(supabaseUrl, supabaseKey, options) {
    warnIfUsingDeprecatedAuthHelpersPackage();
    // singleton client is created only if isSingleton is set to true, or if isSingleton is not defined and we detect a browser
    const shouldUseSingleton = options?.isSingleton === true ||
        ((!options || !("isSingleton" in options)) && isBrowser());
    if (shouldUseSingleton && cachedBrowserClient) {
        return cachedBrowserClient;
    }
    if (!supabaseUrl || !supabaseKey) {
        throw new Error(`@supabase/ssr: Your project's URL and API key are required to create a Supabase client!\n\nCheck your Supabase project's API settings to find these values\n\nhttps://supabase.com/dashboard/project/_/settings/api`);
    }
    if (options?.auth?.storage) {
        warnOnce("@supabase/ssr: createBrowserClient always manages the session via cookies, so the `auth.storage` option you passed is ignored. If you don't need the session to be readable on the server, use @supabase/supabase-js's createClient directly with your own `storage` instead.");
    }
    const { storage } = createStorageFromOptions({
        ...options,
        cookieEncoding: options?.cookieEncoding ?? "base64url",
    }, false);
    const client = createClient(supabaseUrl, supabaseKey, {
        // TODO: resolve type error
        ...options,
        global: {
            ...options?.global,
            headers: {
                ...options?.global?.headers,
                "X-Client-Info": `supabase-ssr/${VERSION} createBrowserClient`,
            },
        },
        auth: {
            ...options?.auth,
            ...(options?.cookieOptions?.name
                ? { storageKey: options.cookieOptions.name }
                : null),
            flowType: "pkce",
            autoRefreshToken: options?.auth?.autoRefreshToken ?? isBrowser(),
            detectSessionInUrl: options?.auth?.detectSessionInUrl ?? isBrowser(),
            persistSession: options?.auth?.persistSession ?? true,
            storage,
            ...(options?.cookies &&
                "encode" in options.cookies &&
                options.cookies.encode === "tokens-only"
                ? {
                    userStorage: options?.auth?.userStorage ??
                        (isBrowser() ? window.localStorage : memoryLocalStorageAdapter()),
                }
                : null),
        },
    });
    if (shouldUseSingleton) {
        cachedBrowserClient = client;
    }
    return client;
}
//# sourceMappingURL=createBrowserClient.js.map