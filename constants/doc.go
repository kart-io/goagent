// Package constants holds the protocol values, field names and default
// parameters shared across goagent packages. It is a leaf package that imports
// only the standard library.
//
// Error codes, categories, messages and error context fields live in the
// errors package. Domain-internal enums and persisted format versions live in
// the packages that own them.
//
// Values documented as former defaults are no longer applied by the framework;
// they are kept so callers can pass the old value explicitly.
package constants
