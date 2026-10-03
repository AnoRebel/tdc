/**
 * Shared type definitions for the TDC platform.
 *
 * This package is transport-agnostic and framework-agnostic: it describes the
 * shapes exchanged between clients and a server, and nothing about how they
 * travel. Both the web and desktop clients depend on it, as does any third
 * party writing an integration.
 */

export * from "./charts";
export * from "./client";
export * from "./apiclient";
export * from "./server";
export * from "./auth";
export * from "./rooms";
export * from "./presence";
export * from "./errors";
export * from "./search";
export * from "./integrations";
export * from "./directory";
export * from "./projects";
