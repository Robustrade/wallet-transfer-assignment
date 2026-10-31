# AI Assistance Disclosure

## Tool Used

**OpenAI ChatGPT (GPT-6)**

## Purpose of AI Assistance

AI assistance was used during the development of the Wallet Transfer Service to support requirement analysis, implementation planning, code review, troubleshooting, and documentation.

## How AI Was Used

ChatGPT was used for the following activities:

1. **Requirement Analysis:** Reviewed the assignment requirements and helped identify the expected functionality, technical constraints, and submission deliverables.
2. **Technical Decisions:** Discussed the database selection and application architecture, including the use of PostgreSQL, Spring Boot, and Spring Data JPA.
3. **Implementation Support:** Assisted with the development structure, REST API implementation, wallet and transfer operations, transaction handling, and database integration.
4. **Reliability Considerations:** Helped review concepts related to idempotency, concurrent transfers, transaction consistency, and error handling.
5. **Testing Support:** Assisted in preparing unit, integration, and concurrency tests and troubleshooting database connectivity during test execution.
6. **Documentation:** Assisted in preparing project documentation, setup instructions, design notes, and this AI assistance disclosure.

## Developer Review and Verification

The developer reviewed the generated implementation, configured the local PostgreSQL environment, and executed the Maven build and verification process.

The following command was executed successfully:

```bash
mvn clean verify
```

The application was also started locally, and its functionality and APIs were tested.

The developer is responsible for understanding and validating the implementation, including database transactions, idempotency, concurrency handling, and failure scenarios, before submission.

## Prompts and Session Record

The relevant ChatGPT prompts and conversation history used during this assignment should be included as the session record, in accordance with the assignment requirements.

The main topics covered during the AI-assisted development session included:

- Reviewing and understanding the wallet transfer assignment.
- Planning the Spring Boot project structure and technology choices.
- Implementing wallet and transfer functionality.
- Handling transaction consistency and idempotent requests.
- Preparing database integration and migration scripts.
- Creating unit, integration, and concurrency tests.
- Troubleshooting PostgreSQL connectivity and Maven test execution.
- Reviewing project documentation and submission requirements.

## Declaration

AI assistance was used as a development aid. The developer remains responsible for reviewing, understanding, validating, and maintaining the submitted source code and for ensuring that the final implementation meets the assignment requirements.