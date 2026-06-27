package embeddings

// symKeySep is the separator byte used to build in-memory (file_path, symbol_name)
// composite keys for the orphan-reconciliation flow (filterSymbols, GetHashes,
// DeleteExplicitOrphans).
//
// NUL ('\x00') is chosen because it cannot appear in a Unix file path or in a
// symbol name produced by the parser, so it unambiguously delimits file from name
// even when either component contains colons (e.g. C++ "::" in names, or unusual
// directory names on non-Windows systems).
//
// This separator is PURELY IN-MEMORY: the DB stores file_path and symbol_name as
// separate columns (PK: repo_key, file_path, symbol_name), so there is no
// persisted format to migrate when this constant changes.
const symKeySep = "\x00"
