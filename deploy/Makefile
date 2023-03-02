MAKE="make"

gcp-setup:
	cp gcp/terraform.tfvars.example gcp/terraform.tfvars && echo "Generated a template terraform.tfvars for you. Fill this in!"

gcp:  
	cd gcp/ && terraform init && terraform apply --auto-approve

destroy-gcp:
	cd gcp/ && terraform destroy --auto-approve
