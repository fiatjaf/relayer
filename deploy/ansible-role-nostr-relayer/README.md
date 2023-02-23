# Ansible role for nostr relayer

This is a ansible role for deploying `relayer`.

## What are Nostr Relays?

Relays are like the backend servers for Nostr. They allow Nostr clients to send them messages, and they may (or may not) store those messages and broadcast those messages to all other connected clients. The world of relays is changing fast so expect many changes here in the future.

 ## sample playbook

 ```yml
- name: "install relayer"
  hosts: enter your hosts file
  become: yes
  role:
    - ansible-role-nostr-relayer
```
