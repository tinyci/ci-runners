This is a simple ansible repository for deploying runners:

- create an `inventory` file that is just a list of hostnames you'd like to install runners on.
- edit `group_vars/all.yml` to taste.
- `ansible-playbook -K -i inventory playbook.yml` to install the runners.
