MAKE="make"

setup-gcp:
	cp gcp/terraform.tfvars.example gcp/terraform.tfvars && echo "Generated a template terraform.tfvars for you. Fill this in!"

create-gcp:  
	cd gcp && terraform init && terraform apply --auto-approve

destroy-gcp:
	cd gcp && terraform destroy --auto-approve
