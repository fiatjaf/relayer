MAKE="make"

setup: 
	cp deploy/gcp/terraform.tfvars.example deploy/gcp/terraform.tfvars && echo "Generated a template terraform.tfvars for you. Fill this in!"

gcp: 
	cd deploy/gcp/ && terraform init && terraform plan && terraform apply --auto-approve

destroy-gcp: 
	cd deploy/gcp/ && terraform destroy --auto-approve
