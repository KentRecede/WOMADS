/**
 * Logs a warning to the console only once per process for each distinct
 * message. Used for configuration warnings that would otherwise fire on
 * every client creation (e.g. once per server request).
 */
export declare function warnOnce(message: string): void;
/**
 * Clears the set of already-logged messages. Only for use in tests.
 */
export declare function resetWarnOnceForTesting(): void;
