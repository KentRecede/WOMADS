"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.warnOnce = warnOnce;
exports.resetWarnOnceForTesting = resetWarnOnceForTesting;
const warnedMessages = new Set();
/**
 * Logs a warning to the console only once per process for each distinct
 * message. Used for configuration warnings that would otherwise fire on
 * every client creation (e.g. once per server request).
 */
function warnOnce(message) {
    if (warnedMessages.has(message)) {
        return;
    }
    warnedMessages.add(message);
    console.warn(message);
}
/**
 * Clears the set of already-logged messages. Only for use in tests.
 */
function resetWarnOnceForTesting() {
    warnedMessages.clear();
}
//# sourceMappingURL=warnOnce.js.map