# TMLIBS

This repo is a home for various small packages.

## autofile

Autofile is file access with automatic log rotation. A group of files is maintained and rotation happens
when the leading file gets too big. Provides a reader for reading from the file group.

## cli

CLI wraps the `cobra` and `viper` packages and handles some common elements of building a CLI like flags and env vars for the home directory and the logger.

## clist

Clist provides a linked list that is safe for concurrent access by many readers.

## common

Common provides a hodgepodge of useful functions.

## events

Events is a synchronous PubSub package.

## flowrate

Flowrate is a fork of https://github.com/mxk/go-flowrate that added a `SetREMA` method.

## log

Log is a log package structured around key-value pairs that allows logging level to be set differently for different keys.

## merkle

Merkle provides a simple static merkle tree and corresponding proofs.

## process

Process is a simple utility for spawning OS processes.

## pubsub

PubSub is an asynchronous PubSub package.
