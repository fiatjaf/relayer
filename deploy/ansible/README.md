# Ansible role for nostr relayer

This is a ansible role for deploying relayer on Ubuntu 22.04 LTS (Jammy Jellyfish)

 ## Sample playbook

```yml
- name: "install relayer"
  hosts: enter your hosts file
  become: yes
  role:
    - ansible-role-nostr-relayer
```
